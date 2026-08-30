package core

import (
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/luynrs/justray/internal/daemon/connection"
	"github.com/luynrs/justray/internal/daemon/engine"
	"github.com/luynrs/justray/internal/daemon/platform/autostart"
	"github.com/luynrs/justray/internal/daemon/store"
	"github.com/luynrs/justray/internal/daemon/subscription"
	"github.com/luynrs/justray/internal/shared/domain"
	"github.com/luynrs/justray/internal/shared/rpc"
)

type Core struct {
	opMu    sync.Mutex
	stateMu sync.RWMutex
	probeMu sync.Mutex
	store   store.Disk
	state   store.PersistentState
	probes  map[domain.NodeRef]engine.Result
	conn    *connection.Service
	subs    *subscription.Service

	jobsMu    sync.Mutex
	refreshes map[string]*refreshCall

	revision atomic.Uint64
	snapshot atomic.Pointer[rpc.Snapshot]
	watchMu  sync.Mutex
	watchers map[chan rpc.Changed]struct{}
}

func New(st store.Disk, conn *connection.Service, subs *subscription.Service, logger *log.Logger) (*Core, error) {
	state, err := st.Load()
	if err != nil {
		return nil, fmt.Errorf("load state: %w", err)
	}
	settings, err := state.Settings.Normalize()
	if err != nil {
		return nil, fmt.Errorf("normalize settings: %w", err)
	}
	if autostart.Enabled() {
		settings.Autostart = "on"
	}
	state.Settings = settings
	c := &Core{store: st, state: state, probes: map[domain.NodeRef]engine.Result{}, conn: conn, subs: subs, refreshes: map[string]*refreshCall{}, watchers: map[chan rpc.Changed]struct{}{}}
	c.publish()
	return c, nil
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
	c.conn.Restore(n, ref, state.Settings, state.Tun)
	c.publish()
}

func (c *Core) Shutdown() {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	c.conn.Shutdown()
	c.publish()
}

func (c *Core) Snapshot() rpc.Snapshot            { return cloneSnapshot(*c.snapshot.Load()) }
func (c *Core) RestartRequested() <-chan struct{} { return c.conn.RestartRequested() }

func (c *Core) Watch() (rpc.Changed, <-chan rpc.Changed, func()) {
	ch := make(chan rpc.Changed, 1)
	c.watchMu.Lock()
	c.watchers[ch] = struct{}{}
	c.watchMu.Unlock()
	return rpc.Changed{Revision: c.Snapshot().Revision}, ch, func() {
		c.watchMu.Lock()
		delete(c.watchers, ch)
		c.watchMu.Unlock()
	}
}

func (c *Core) Probe(ctx context.Context, sub, id string) error {
	c.probeMu.Lock()
	defer c.probeMu.Unlock()

	state := c.current()
	refs, nodes, err := probeTargets(state.Subscriptions, sub, id)
	if err != nil {
		return err
	}
	results, err := c.conn.Probe(ctx, nodes, state.Settings)
	if err != nil {
		return err
	}
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	live := map[domain.NodeRef]bool{}
	for _, subscription := range c.current().Subscriptions {
		for _, node := range subscription.Nodes {
			live[domain.NodeRef{SubscriptionID: subscription.ID, NodeID: node.ID}] = true
		}
	}
	for _, ref := range refs {
		if result, ok := results[ref.NodeID]; ok && live[ref] {
			c.probes[ref] = result
		}
	}
	c.publish()
	return nil
}

func (c *Core) AddSubscription(ctx context.Context, rawURL string) (rpc.Sub, error) {
	sub, err := c.subs.PrepareAdd(ctx, rawURL)
	if err != nil {
		return rpc.Sub{}, err
	}
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if err := ctx.Err(); err != nil {
		return rpc.Sub{}, err
	}
	next := c.current()
	next.Subscriptions = append(next.Subscriptions, sub)
	if err := c.commit(next); err != nil {
		return rpc.Sub{}, err
	}
	c.publish()
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
	cleanupErr := c.conn.ForgetIfRemoved(id)
	c.publish()
	return cleanupErr
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
	if err := c.commit(next); err != nil {
		return err
	}
	c.publish()
	return nil
}

func (c *Core) RefreshSubscriptions(ctx context.Context) ([]rpc.Sub, error) {
	subs := c.current().Subscriptions
	out, updated, refreshErr := c.subs.RefreshAll(ctx, subs, c.refresh)
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	next := c.current()
	var dropConn bool
	for _, sub := range updated {
		if i := slices.IndexFunc(next.Subscriptions, func(current store.Subscription) bool { return current.ID == sub.ID }); i >= 0 {
			next.Subscriptions[i] = sub
			if c.sanitizeRefs(&next, sub) {
				dropConn = true
			}
		}
	}
	syncErr := c.syncAfterRefresh(ctx, next, dropConn)
	return out, errors.Join(refreshErr, syncErr)
}

func (c *Core) RefreshSubscription(ctx context.Context, id string) (rpc.Sub, error) {
	state := c.current()
	i := slices.IndexFunc(state.Subscriptions, func(sub store.Subscription) bool { return sub.ID == id })
	if i < 0 {
		return rpc.Sub{}, fmt.Errorf("subscription %q not found", id)
	}
	sub, err := c.refresh(ctx, state.Subscriptions[i])
	if err != nil {
		return rpc.Sub{}, err
	}
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if err := ctx.Err(); err != nil {
		return rpc.Sub{}, err
	}
	next := c.current()
	i = slices.IndexFunc(next.Subscriptions, func(current store.Subscription) bool { return current.ID == id })
	if i < 0 {
		return rpc.Sub{}, fmt.Errorf("subscription %q not found", id)
	}
	next.Subscriptions[i] = sub
	if err := c.syncAfterRefresh(ctx, next, c.sanitizeRefs(&next, sub)); err != nil {
		return rpc.Sub{}, err
	}
	return subscription.Info(sub), nil
}

func (c *Core) syncAfterRefresh(ctx context.Context, next store.PersistentState, dropConn bool) error {
	if err := c.commit(next); err != nil {
		return err
	}
	var runtimeErr error
	if dropConn {
		_, runtimeErr = c.conn.Disconnect(ctx)
	} else {
		_, runtimeErr = c.apply(ctx, next)
	}
	c.publish()
	return runtimeErr
}

func (c *Core) sanitizeRefs(state *store.PersistentState, updated store.Subscription) bool {
	nodeExists := func(ref domain.NodeRef) bool {
		return slices.ContainsFunc(updated.Nodes, func(n domain.Node) bool { return n.ID == ref.NodeID })
	}
	var dropConn bool
	if state.Active.SubscriptionID == updated.ID && state.Active.NodeID != "" && !nodeExists(state.Active) {
		state.Active = domain.NodeRef{}
		dropConn = true
	}
	if state.Last.SubscriptionID == updated.ID && state.Last.NodeID != "" && !nodeExists(state.Last) {
		state.Last = domain.NodeRef{}
	}
	return dropConn
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

func (c *Core) Connect(ctx context.Context, nodeID, subscriptionID string) (rpc.Status, error) {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if err := ctx.Err(); err != nil {
		return c.status(c.current()), err
	}
	next := c.current()
	n, ref, err := find(next.Subscriptions, domain.NodeRef{SubscriptionID: subscriptionID, NodeID: nodeID})
	if err != nil {
		return c.status(next), err
	}
	next.Active, next.Last = ref, ref
	if saveErr := c.commit(next); saveErr != nil {
		return c.status(next), saveErr
	}
	_, applyErr := c.conn.Connect(ctx, n, ref, next.Settings, next.Tun)
	c.publish()
	return c.status(next), applyErr
}

func (c *Core) Disconnect(ctx context.Context) (rpc.Status, error) {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if err := ctx.Err(); err != nil {
		return c.status(c.current()), err
	}
	next := c.current()
	next.Active = domain.NodeRef{}
	if err := c.commit(next); err != nil {
		return c.status(next), err
	}
	_, applyErr := c.conn.Disconnect(ctx)
	c.publish()
	return c.status(next), applyErr
}

func (c *Core) SetTun(ctx context.Context, enable bool) (rpc.Status, error) {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if err := ctx.Err(); err != nil {
		return c.status(c.current()), err
	}
	next := c.current()
	next.Tun = enable
	if err := c.commit(next); err != nil {
		return c.status(next), err
	}
	_, applyErr := c.apply(ctx, next)
	c.publish()
	return c.status(next), applyErr
}

func (c *Core) SetSettings(ctx context.Context, settings domain.Settings) (rpc.Status, error) {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if err := ctx.Err(); err != nil {
		return c.status(c.current()), err
	}
	settings, err := settings.Normalize()
	if err != nil {
		return c.status(c.current()), err
	}
	old := c.current().Settings
	next := c.current()
	next.Settings = settings
	if err := c.commit(next); err != nil {
		return c.status(c.current()), err
	}
	if settings.Autostart != old.Autostart {
		apply := autostart.Enable
		if settings.Autostart == "off" {
			apply = autostart.Disable
		}
		if err := apply(); err != nil {
			next.Settings.Autostart = old.Autostart
			_ = c.commit(next)
			c.publish()
			return c.status(next), err
		}
	}
	_, applyErr := c.apply(ctx, next)
	c.publish()
	return c.status(next), applyErr
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

func (c *Core) publish() {
	state := c.current()
	subs := make([]rpc.Sub, len(state.Subscriptions))
	for i, sub := range state.Subscriptions {
		subs[i] = subscription.Info(sub)
	}
	active := state.Active
	if active.NodeID == "" {
		active = state.Last
	}
	snapshot := &rpc.Snapshot{
		Revision:      c.revision.Add(1),
		Settings:      cloneSettings(state.Settings),
		Subscriptions: subs,
		Nodes:         c.nodes(state.Subscriptions),
		Status:        c.status(state),
		Active:        active,
	}
	c.snapshot.Store(snapshot)
	c.watchMu.Lock()
	for ch := range c.watchers {
		select {
		case ch <- rpc.Changed{Revision: snapshot.Revision}:
		default:
		}
	}
	c.watchMu.Unlock()
}

func (c *Core) apply(ctx context.Context, state store.PersistentState) (rpc.Status, error) {
	if !c.conn.Status().Connected {
		return c.status(state), nil
	}
	node, ref, err := find(state.Subscriptions, state.Active)
	if err != nil {
		return c.status(state), err
	}
	return c.conn.Apply(ctx, node, ref, state.Settings, state.Tun)
}

func (c *Core) status(state store.PersistentState) rpc.Status {
	status := c.conn.Status()
	if !status.Connected {
		status.Port = state.Settings.Port
		status.Tun = state.Tun
	}
	return status
}

func (c *Core) nodes(subscriptions []store.Subscription) []rpc.Node {
	live := map[domain.NodeRef]bool{}
	out := []rpc.Node{}
	for _, subscription := range subscriptions {
		for _, node := range subscription.Nodes {
			ref := domain.NodeRef{SubscriptionID: subscription.ID, NodeID: node.ID}
			live[ref] = true
			item := rpc.Node{ID: node.ID, Name: node.Name, Protocol: string(node.Protocol), Server: node.Server, Port: node.Port, Sub: subscription.ID}
			if result, ok := c.probes[ref]; ok {
				item.Probed, item.Alive, item.MS = true, result.Alive, result.MS
			}
			out = append(out, item)
		}
	}
	for ref := range c.probes {
		if !live[ref] {
			delete(c.probes, ref)
		}
	}
	return out
}

func probeTargets(subscriptions []store.Subscription, subID, nodeID string) ([]domain.NodeRef, []domain.Node, error) {
	var refs []domain.NodeRef
	var nodes []domain.Node
	for _, subscription := range subscriptions {
		if subID != "" && subscription.ID != subID {
			continue
		}
		for _, node := range subscription.Nodes {
			if nodeID != "" && node.ID != nodeID {
				continue
			}
			refs = append(refs, domain.NodeRef{SubscriptionID: subscription.ID, NodeID: node.ID})
			nodes = append(nodes, node)
		}
	}
	if len(nodes) == 0 {
		return nil, nil, fmt.Errorf("nothing to probe")
	}
	return refs, nodes, nil
}

func cloneSnapshot(snapshot rpc.Snapshot) rpc.Snapshot {
	snapshot.Settings = cloneSettings(snapshot.Settings)
	snapshot.Subscriptions = slices.Clone(snapshot.Subscriptions)
	snapshot.Nodes = slices.Clone(snapshot.Nodes)
	return snapshot
}

func cloneSettings(settings domain.Settings) domain.Settings {
	settings.Except = slices.Clone(settings.Except)
	settings.Blocked = slices.Clone(settings.Blocked)
	return settings
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
