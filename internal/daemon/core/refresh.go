package core

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/luynrs/justray/internal/daemon/store"
	"github.com/luynrs/justray/internal/domain"
)

type refreshCall struct {
	done chan struct{}
	err  error
}

func (c *Core) RefreshSubscriptions(ctx context.Context, ids ...string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	subs := c.current().Subscriptions
	var refreshErr error
	for _, id := range ids {
		if !slices.ContainsFunc(subs, func(sub store.Subscription) bool { return sub.ID == id && sub.URL != "" }) {
			refreshErr = errors.Join(refreshErr, fmt.Errorf("subscription %q not found", id))
		}
	}
	subs = slices.DeleteFunc(subs, func(sub store.Subscription) bool {
		return sub.URL == "" || len(ids) > 0 && !slices.Contains(ids, sub.ID)
	})
	if len(subs) == 0 {
		return refreshErr
	}
	errs := make([]error, len(subs))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range min(8, len(subs)) {
		wg.Go(func() {
			for i := range jobs {
				errs[i] = c.refresh(ctx, subs[i])
			}
		})
	}
dispatch:
	for i := range subs {
		select {
		case jobs <- i:
		case <-ctx.Done():
			break dispatch
		}
	}
	close(jobs)
	wg.Wait()
	for i, sub := range subs {
		if errs[i] != nil {
			refreshErr = errors.Join(refreshErr, fmt.Errorf("%s: %w", sub.ID, errs[i]))
		}
	}
	return errors.Join(refreshErr, ctx.Err())
}

func (c *Core) sanitizeRefs(state *store.State, updated store.Subscription) bool {
	nodeExists := func(ref domain.NodeRef) bool {
		return slices.ContainsFunc(updated.Nodes, func(n domain.Node) bool { return n.ID == ref.NodeID })
	}
	status := c.conn.Status()
	for _, ref := range []*domain.NodeRef{&state.Active, &state.Last} {
		if ref.SubscriptionID == updated.ID && ref.NodeID != "" && !nodeExists(*ref) {
			*ref = domain.NodeRef{}
		}
	}
	if state.Pending != nil && state.Pending.Ref.SubscriptionID == updated.ID && !nodeExists(state.Pending.Ref) {
		state.Pending = nil
	}
	return status.Connected && status.NodeRef.SubscriptionID == updated.ID && !nodeExists(status.NodeRef)
}

func (c *Core) refresh(ctx context.Context, sub store.Subscription) (err error) {
	var call *refreshCall
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		c.stMu.Lock()
		call = c.refreshes[sub.ID]
		if call == nil {
			index := slices.IndexFunc(c.state.Subscriptions, func(current store.Subscription) bool { return current.ID == sub.ID })
			if index < 0 {
				c.stMu.Unlock()
				return fmt.Errorf("subscription %q not found", sub.ID)
			}
			sub = c.state.Subscriptions[index]
			call = &refreshCall{done: make(chan struct{})}
			c.refreshes[sub.ID] = call
			c.publishLocked()
			c.stMu.Unlock()
			break
		}
		c.stMu.Unlock()
		select {
		case <-call.done:
			if call.err != context.Canceled && call.err != context.DeadlineExceeded {
				return call.err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	defer func() {
		c.stMu.Lock()
		call.err = err
		if err != nil && ctx.Err() != nil {
			call.err = ctx.Err()
		}
		delete(c.refreshes, sub.ID)
		c.publishLocked()
		close(call.done)
		c.stMu.Unlock()
	}()

	sub, err = c.subs.Refresh(ctx, sub)
	if err != nil {
		return err
	}
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	next := c.current()
	index := slices.IndexFunc(next.Subscriptions, func(current store.Subscription) bool { return current.ID == sub.ID })
	if index < 0 {
		return fmt.Errorf("subscription %q not found", sub.ID)
	}
	sub.Nodes = assignNodeIDs(sub.Nodes, next.Subscriptions[index].Nodes)
	next.Subscriptions[index] = sub
	dropConn := c.sanitizeRefs(&next, sub)
	if err := c.commit(next); err != nil {
		return err
	}
	ctx = context.WithoutCancel(ctx)
	if dropConn {
		return c.conn.Disconnect(ctx)
	}
	if status := c.conn.Status(); status.Connected && status.NodeRef.SubscriptionID == sub.ID {
		node, ref, err := find(next.Subscriptions, status.NodeRef)
		if err != nil {
			return err
		}
		return c.conn.Apply(ctx, node, ref, next.Settings, status.Tun)
	}
	return nil
}
