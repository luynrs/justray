// Package core owns daemon operations and their serialization.
package core

import (
	"context"
	"sync"

	"github.com/luynrs/justray/internal/daemon/connection"
	"github.com/luynrs/justray/internal/daemon/subscription"
	"github.com/luynrs/justray/internal/shared/domain"
	"github.com/luynrs/justray/internal/shared/rpc"
)

type Core struct {
	// ponytail: subscription I/O holds this lock until fetch and commit are split
	opMu sync.Mutex
	conn *connection.Service
	subs *subscription.Service
}

func New(conn *connection.Service, subs *subscription.Service) *Core {
	return &Core{conn: conn, subs: subs}
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
	c.opMu.Lock()
	defer c.opMu.Unlock()
	return c.subs.Add(ctx, rawURL)
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
	c.opMu.Lock()
	defer c.opMu.Unlock()
	return c.subs.RefreshAll(ctx)
}

func (c *Core) RefreshSubscription(ctx context.Context, id string) (rpc.Sub, error) {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	return c.subs.Refresh(ctx, id)
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
