// Package core owns daemon operations and their serialization.
package core

import (
	"context"
	"fmt"
	"sync"

	"github.com/luynrs/justray/internal/daemon/connection"
	"github.com/luynrs/justray/internal/daemon/store"
	"github.com/luynrs/justray/internal/daemon/subscription"
	"github.com/luynrs/justray/internal/shared/domain"
	"github.com/luynrs/justray/internal/shared/rpc"
)

type Core struct {
	opMu sync.Mutex
	conn *connection.Service
	subs *subscription.Service

	jobsMu    sync.Mutex
	refreshes map[string]*refreshCall
}

func New(conn *connection.Service, subs *subscription.Service) *Core {
	return &Core{conn: conn, subs: subs, refreshes: map[string]*refreshCall{}}
}

type refreshCall struct {
	done chan struct{}
	sub  store.Subscription
	err  error
}

func (c *Core) Restore() {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	c.conn.Restore()
}

func (c *Core) Shutdown() {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	c.conn.Shutdown()
}

func (c *Core) Status() rpc.Status                             { return c.conn.Status() }
func (c *Core) ActiveRef() (domain.NodeRef, error)             { return c.conn.ActiveRef() }
func (c *Core) Subscriptions() ([]rpc.Sub, error)              { return c.subs.List() }
func (c *Core) Nodes() ([]rpc.Node, error)                     { return c.conn.Nodes() }
func (c *Core) Settings() domain.Settings                      { return c.conn.Settings() }
func (c *Core) RefreshEvery() int                              { return c.conn.RefreshEvery() }
func (c *Core) Watch() (rpc.Status, <-chan rpc.Status, func()) { return c.conn.Watch() }
func (c *Core) RestartRequested() <-chan struct{}              { return c.conn.RestartRequested() }

func (c *Core) Probe(ctx context.Context, sub, id string) ([]rpc.Node, error) {
	return c.conn.Probe(ctx, sub, id)
}

func (c *Core) AddSubscription(ctx context.Context, rawURL string) (rpc.Sub, error) {
	sub, err := c.subs.PrepareAdd(ctx, rawURL)
	if err != nil {
		return rpc.Sub{}, err
	}
	c.opMu.Lock()
	defer c.opMu.Unlock()
	return c.subs.Add(sub)
}

func (c *Core) RemoveSubscription(id string) error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	sub, err := c.subs.Remove(id)
	if err != nil {
		return err
	}
	c.conn.ForgetIfRemoved(sub.ID, sub.Nodes)
	return nil
}

func (c *Core) MoveSubscription(id string, dir int) error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	return c.subs.MoveSub(id, dir)
}

func (c *Core) RefreshSubscriptions(ctx context.Context) ([]rpc.Sub, error) {
	subs, err := c.subs.All()
	if err != nil {
		return nil, err
	}
	out, updated, refreshErr := c.subs.RefreshAll(ctx, subs, c.refresh)
	c.opMu.Lock()
	_, err = c.subs.Commit(updated)
	c.opMu.Unlock()
	if err != nil {
		return nil, err
	}
	return out, refreshErr
}

func (c *Core) RefreshSubscription(ctx context.Context, id string) (rpc.Sub, error) {
	sub, err := c.subs.Get(id)
	if err != nil {
		return rpc.Sub{}, err
	}
	sub, err = c.refresh(ctx, sub)
	if err != nil {
		return rpc.Sub{}, err
	}
	c.opMu.Lock()
	committed, err := c.subs.Commit([]store.Subscription{sub})
	c.opMu.Unlock()
	if err != nil {
		return rpc.Sub{}, err
	}
	if committed == 0 {
		return rpc.Sub{}, fmt.Errorf("subscription %q not found", id)
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
	return c.conn.Connect(nodeID, subscriptionID)
}

func (c *Core) Disconnect() (rpc.Status, error) {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	return c.conn.Disconnect()
}

func (c *Core) SetTun(enable bool) (rpc.Status, error) {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	return c.conn.SetTun(enable)
}

func (c *Core) SetSettings(settings domain.Settings) (rpc.Status, error) {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	return c.conn.SetSettings(settings)
}
