// Package core owns daemon operations and their serialization.
package core

import (
	"context"
	"fmt"
	"log"
	"slices"
	"strings"
	"sync"

	"github.com/luynrs/justray/internal/daemon/connection"
	"github.com/luynrs/justray/internal/daemon/platform/autostart"
	"github.com/luynrs/justray/internal/daemon/store"
	"github.com/luynrs/justray/internal/daemon/subscription"
	"github.com/luynrs/justray/internal/shared/domain"
	"github.com/luynrs/justray/internal/shared/rpc"
)

type Core struct {
	opMu    sync.Mutex
	stateMu sync.RWMutex
	store   store.Disk
	state   store.PersistentState
	conn    *connection.Service
	subs    *subscription.Service

	jobsMu    sync.Mutex
	refreshes map[string]*refreshCall
}

func New(st store.Disk, conn *connection.Service, subs *subscription.Service, logger *log.Logger) *Core {
	state, err := st.Load()
	if err != nil {
		logger.Print(err)
	}
	settings, err := state.Settings.Normalize()
	if err != nil {
		logger.Print(err)
		settings, _ = domain.Settings{}.Normalize()
	}
	if autostart.Enabled() {
		settings.Autostart = "on"
	}
	state.Settings = settings
	conn.Configure(settings, state.Tun)
	return &Core{store: st, state: state, conn: conn, subs: subs, refreshes: map[string]*refreshCall{}}
}

type refreshCall struct {
	done chan struct{}
	sub  store.Subscription
	err  error
}

func (c *Core) Restore() {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	state := c.current()
	if state.Active.NodeID == "" {
		return
	}
	n, ref, err := find(state.Subscriptions, state.Active)
	if err != nil {
		return
	}
	c.conn.Restore(n, ref)
}

func (c *Core) Shutdown() {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	c.conn.Shutdown()
}

func (c *Core) Status() rpc.Status { return c.conn.Status() }
func (c *Core) ActiveRef() (domain.NodeRef, error) {
	state := c.current()
	if state.Active.NodeID != "" {
		return state.Active, nil
	}
	return state.Last, nil
}
func (c *Core) Subscriptions() ([]rpc.Sub, error) {
	state := c.current()
	out := make([]rpc.Sub, len(state.Subscriptions))
	for i, sub := range state.Subscriptions {
		out[i] = subscription.Info(sub)
	}
	return out, nil
}
func (c *Core) Nodes() ([]rpc.Node, error)                     { return c.conn.Nodes(c.current().Subscriptions), nil }
func (c *Core) Settings() domain.Settings                      { return c.current().Settings }
func (c *Core) RefreshEvery() int                              { return c.current().Settings.RefreshEvery }
func (c *Core) Watch() (rpc.Status, <-chan rpc.Status, func()) { return c.conn.Watch() }
func (c *Core) RestartRequested() <-chan struct{}              { return c.conn.RestartRequested() }

func (c *Core) Probe(ctx context.Context, sub, id string) ([]rpc.Node, error) {
	return c.conn.Probe(ctx, c.current().Subscriptions, sub, id)
}

func (c *Core) AddSubscription(ctx context.Context, rawURL string) (rpc.Sub, error) {
	sub, err := c.subs.PrepareAdd(ctx, rawURL)
	if err != nil {
		return rpc.Sub{}, err
	}
	c.opMu.Lock()
	defer c.opMu.Unlock()
	next := c.current()
	next.Subscriptions = append(next.Subscriptions, sub)
	if err := c.commit(next); err != nil {
		return rpc.Sub{}, err
	}
	return subscription.Info(sub), nil
}

func (c *Core) RemoveSubscription(id string) error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	next := c.current()
	i := slices.IndexFunc(next.Subscriptions, func(sub store.Subscription) bool { return sub.ID == id })
	if i < 0 {
		return fmt.Errorf("subscription %q not found", id)
	}
	removed := next.Subscriptions[i]
	next.Subscriptions = slices.Delete(next.Subscriptions, i, i+1)
	belongs := func(ref domain.NodeRef) bool {
		return ref.NodeID != "" && (ref.SubscriptionID == id || ref.SubscriptionID == "" && slices.ContainsFunc(removed.Nodes, func(n domain.Node) bool { return n.ID == ref.NodeID }))
	}
	if belongs(next.Active) {
		next.Active = domain.NodeRef{}
	}
	if belongs(next.Last) {
		next.Last = domain.NodeRef{}
	}
	if err := c.commit(next); err != nil {
		return err
	}
	c.conn.ForgetIfRemoved(id)
	return nil
}

func (c *Core) MoveSubscription(id string, dir int) error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	next := c.current()
	i := slices.IndexFunc(next.Subscriptions, func(sub store.Subscription) bool { return sub.ID == id })
	if i < 0 {
		return fmt.Errorf("subscription %q not found", id)
	}
	j := i + dir
	if j < 0 || j >= len(next.Subscriptions) {
		return nil
	}
	next.Subscriptions[i], next.Subscriptions[j] = next.Subscriptions[j], next.Subscriptions[i]
	return c.commit(next)
}

func (c *Core) RefreshSubscriptions(ctx context.Context) ([]rpc.Sub, error) {
	subs := c.current().Subscriptions
	out, updated, refreshErr := c.subs.RefreshAll(ctx, subs, c.refresh)
	c.opMu.Lock()
	next := c.current()
	for _, sub := range updated {
		if i := slices.IndexFunc(next.Subscriptions, func(current store.Subscription) bool { return current.ID == sub.ID }); i >= 0 {
			next.Subscriptions[i] = sub
		}
	}
	err := c.commit(next)
	c.opMu.Unlock()
	if err != nil {
		return nil, err
	}
	return out, refreshErr
}

func (c *Core) RefreshSubscription(ctx context.Context, id string) (rpc.Sub, error) {
	state := c.current()
	i := slices.IndexFunc(state.Subscriptions, func(sub store.Subscription) bool { return sub.ID == id })
	if i < 0 {
		return rpc.Sub{}, fmt.Errorf("subscription %q not found", id)
	}
	sub := state.Subscriptions[i]
	var err error
	sub, err = c.refresh(ctx, sub)
	if err != nil {
		return rpc.Sub{}, err
	}
	c.opMu.Lock()
	next := c.current()
	i = slices.IndexFunc(next.Subscriptions, func(current store.Subscription) bool { return current.ID == id })
	if i >= 0 {
		next.Subscriptions[i] = sub
	}
	if i < 0 {
		c.opMu.Unlock()
		return rpc.Sub{}, fmt.Errorf("subscription %q not found", id)
	}
	err = c.commit(next)
	c.opMu.Unlock()
	if err != nil {
		return rpc.Sub{}, err
	}
	return subscription.Info(sub), nil
}

func (c *Core) refresh(ctx context.Context, sub store.Subscription) (store.Subscription, error) {
	c.jobsMu.Lock()
	if call := c.refreshes[sub.ID]; call != nil {
		c.jobsMu.Unlock()
		select {
		case <-call.done:
			return call.sub, call.err
		case <-ctx.Done():
			return sub, ctx.Err()
		}
	}
	call := &refreshCall{done: make(chan struct{})}
	c.refreshes[sub.ID] = call
	c.jobsMu.Unlock()

	call.sub, call.err = c.subs.Refresh(ctx, sub)
	c.jobsMu.Lock()
	delete(c.refreshes, sub.ID)
	close(call.done)
	c.jobsMu.Unlock()
	return call.sub, call.err
}

func (c *Core) Connect(nodeID, subscriptionID string) (rpc.Status, error) {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	next := c.current()
	n, ref, err := find(next.Subscriptions, domain.NodeRef{SubscriptionID: subscriptionID, NodeID: nodeID})
	if err != nil {
		return c.conn.Status(), err
	}
	status, err := c.conn.Connect(n, ref)
	if err != nil && err != rpc.ErrElevate {
		return status, err
	}
	next.Active, next.Last = ref, ref
	if saveErr := c.commit(next); saveErr != nil {
		return status, saveErr
	}
	return status, err
}

func (c *Core) Disconnect() (rpc.Status, error) {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	status, err := c.conn.Disconnect()
	if err != nil {
		return status, err
	}
	next := c.current()
	next.Active = domain.NodeRef{}
	if err := c.commit(next); err != nil {
		return status, err
	}
	return status, nil
}

func (c *Core) SetTun(enable bool) (rpc.Status, error) {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	status, err := c.conn.SetTun(enable)
	if err != nil {
		return status, err
	}
	next := c.current()
	next.Tun = enable
	if err := c.commit(next); err != nil {
		return status, err
	}
	return status, nil
}

func (c *Core) SetSettings(settings domain.Settings) (rpc.Status, error) {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	settings, err := settings.Normalize()
	if err != nil {
		return c.conn.Status(), err
	}
	old := c.current().Settings
	if settings.Autostart != old.Autostart {
		apply := autostart.Enable
		if settings.Autostart == "off" {
			apply = autostart.Disable
		}
		if err := apply(); err != nil {
			return c.conn.Status(), err
		}
	}
	next := c.current()
	next.Settings = settings
	if err := c.commit(next); err != nil {
		return c.conn.Status(), err
	}
	return c.conn.ApplySettings(settings)
}

func (c *Core) current() store.PersistentState {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	state := c.state
	state.Subscriptions = slices.Clone(state.Subscriptions)
	return state
}

func (c *Core) commit(state store.PersistentState) error {
	if err := c.store.Save(state); err != nil {
		return err
	}
	c.stateMu.Lock()
	c.state = state
	c.stateMu.Unlock()
	return nil
}

func find(subs []store.Subscription, query domain.NodeRef) (domain.Node, domain.NodeRef, error) {
	if query.NodeID == "" {
		return domain.Node{}, domain.NodeRef{}, fmt.Errorf("node not found")
	}
	var node domain.Node
	var ref domain.NodeRef
	for _, sub := range subs {
		if query.SubscriptionID != "" && sub.ID != query.SubscriptionID {
			continue
		}
		for _, n := range sub.Nodes {
			if !strings.HasPrefix(n.ID, query.NodeID) {
				continue
			}
			if ref.NodeID != "" {
				return domain.Node{}, domain.NodeRef{}, fmt.Errorf("ambiguous node ID %q", query.NodeID)
			}
			node, ref = n, domain.NodeRef{SubscriptionID: sub.ID, NodeID: n.ID}
		}
	}
	if ref.NodeID == "" {
		return domain.Node{}, domain.NodeRef{}, fmt.Errorf("node %q not found", query.NodeID)
	}
	return node, ref, nil
}
