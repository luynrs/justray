package core

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/luynrs/justray/internal/daemon/store"
	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/ipc"
)

func (c *Core) Restore() error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	state := c.current()
	if state.Pending != nil {
		if node, ref, err := find(state.Subscriptions, state.Pending.Ref); err == nil {
			if err := c.conn.Restore(node, ref, state.Settings, state.Pending.Tun); errors.Is(err, ipc.ErrElevate) {
				return err
			}
			if status := c.conn.Status(); status.Connected && status.NodeRef == ref && status.Tun == state.Pending.Tun {
				state.Active, state.Last, state.Tun = ref, ref, state.Pending.Tun
			}
		}
		state.Pending = nil
		if err := c.commit(state); err != nil {
			return err
		}
	}
	if !c.conn.Status().Connected && state.Active.NodeID != "" {
		if node, ref, err := find(state.Subscriptions, state.Active); err == nil {
			if err := c.conn.Restore(node, ref, state.Settings, state.Tun); errors.Is(err, ipc.ErrElevate) {
				return err
			}
		}
	}
	c.publish()
	return nil
}

func (c *Core) RestoreFailed(err error) {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	state := c.current()
	if state.Pending != nil {
		state.Pending = nil
		err = errors.Join(err, c.commit(state))
		if !state.Tun && state.Active.NodeID != "" {
			if node, ref, findErr := find(state.Subscriptions, state.Active); findErr == nil {
				err = errors.Join(err, c.conn.Restore(node, ref, state.Settings, false))
			}
		}
	}
	c.conn.SetError(err)
	c.publish()
}

func (c *Core) RestartRequested() <-chan struct{} { return c.conn.RestartRequested() }

type elevationRequest struct{ *store.Pending }

func (*elevationRequest) Error() string { return ipc.ErrElevate.Error() }
func (*elevationRequest) Unwrap() error { return ipc.ErrElevate }

func (c *Core) RequestRestart(err error) {
	request, ok := err.(*elevationRequest)
	if !ok {
		return
	}
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if c.current().Pending == request.Pending {
		c.conn.RequestRestart()
	}
}

func (c *Core) Connect(ctx context.Context, nodeID, subscriptionID string, mode *bool) error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	previous := c.current()
	before := c.conn.Status()
	if before.Connected {
		previous.Active, previous.Last, previous.Tun = before.NodeRef, before.NodeRef, before.Tun
	}
	previous.Pending = nil
	next := previous
	n, ref, err := find(next.Subscriptions, domain.NodeRef{SubscriptionID: subscriptionID, NodeID: nodeID})
	if err != nil {
		return err
	}
	next.Active, next.Last = ref, ref
	if mode != nil {
		next.Tun = *mode
	}
	return c.finishConnection(ctx, previous, next, before, c.conn.Connect(ctx, n, ref, next.Settings, next.Tun))
}

func (c *Core) Disconnect(ctx context.Context) error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	next := c.current()
	next.Active = domain.NodeRef{}
	next.Pending = nil
	return c.finishDisconnect(ctx, next, true)
}

func (c *Core) finishDisconnect(ctx context.Context, next store.State, dropConn bool) error {
	if err := c.commit(next); err != nil {
		return err
	}
	var disconnectErr error
	if dropConn {
		disconnectErr = c.conn.Disconnect(context.WithoutCancel(ctx))
	}
	c.publish()
	return disconnectErr
}

func (c *Core) SetTun(ctx context.Context, enable bool) error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	previous := c.current()
	before := c.conn.Status()
	if before.Connected {
		previous.Active, previous.Last, previous.Tun = before.NodeRef, before.NodeRef, before.Tun
	}
	previous.Pending = nil
	next := previous
	next.Tun = enable
	var applyErr error
	if before.Connected && before.Tun != enable {
		node, ref, err := find(next.Subscriptions, before.NodeRef)
		if err != nil {
			return err
		}
		applyErr = c.conn.Apply(ctx, node, ref, next.Settings, enable)
	}
	return c.finishConnection(ctx, previous, next, before, applyErr)
}

func (c *Core) finishConnection(ctx context.Context, previous, next store.State, before ipc.Status, err error) error {
	if cancellation := ctx.Err(); cancellation != nil {
		err = cancellation
	}
	switch {
	case err == nil:
		if saveErr := c.commit(next); saveErr != nil {
			err = errors.Join(saveErr, c.restoreLive(ctx, previous, before))
		}
	case errors.Is(err, ipc.ErrElevate):
		previous.Pending = &store.Pending{Ref: next.Active, Tun: next.Tun}
		if saveErr := c.commit(previous); saveErr != nil {
			err = errors.Join(saveErr, c.restoreLive(ctx, previous, before))
		} else {
			err = &elevationRequest{previous.Pending}
		}
	default:
		saveErr := c.commit(previous)
		err = errors.Join(err, saveErr, c.restoreLive(ctx, previous, before))
	}
	if err == nil || errors.Is(err, ipc.ErrElevate) {
		c.conn.SetError(nil)
	}
	c.publish()
	return err
}

func (c *Core) restoreLive(ctx context.Context, state store.State, before ipc.Status) error {
	status := c.conn.Status()
	if status.Connected == before.Connected && status.NodeRef == before.NodeRef && status.Tun == before.Tun {
		return nil
	}
	ctx = context.WithoutCancel(ctx)
	if !before.Connected {
		return c.conn.Disconnect(ctx)
	}
	node, ref, err := find(state.Subscriptions, before.NodeRef)
	if err != nil {
		return err
	}
	return c.conn.Connect(ctx, node, ref, state.Settings, before.Tun)
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
			if ref.NodeID != "" && ref != (domain.NodeRef{SubscriptionID: sub.ID, NodeID: n.ID}) {
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
