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

func (c *Core) sanitizeRefs(state *store.PersistentState, updated store.Subscription) bool {
	nodeExists := func(ref domain.NodeRef) bool {
		return slices.ContainsFunc(updated.Nodes, func(n domain.Node) bool { return n.ID == ref.NodeID })
	}
	status := c.conn.Status()
	dropConn := status.Connected && status.NodeRef.SubscriptionID == updated.ID && !nodeExists(status.NodeRef)
	if state.Active.SubscriptionID == updated.ID && state.Active.NodeID != "" && !nodeExists(state.Active) {
		state.Active = domain.NodeRef{}
	}
	if state.Last.SubscriptionID == updated.ID && state.Last.NodeID != "" && !nodeExists(state.Last) {
		state.Last = domain.NodeRef{}
	}
	return dropConn
}

func (c *Core) refresh(ctx context.Context, sub store.Subscription) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.pubMu.Lock()
	if call := c.refreshes[sub.ID]; call != nil {
		c.pubMu.Unlock()
		select {
		case <-call.done:
			return call.err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	call := &refreshCall{done: make(chan struct{})}
	c.refreshes[sub.ID] = call
	c.publishLocked()
	c.pubMu.Unlock()
	defer func() {
		c.pubMu.Lock()
		call.err = err
		delete(c.refreshes, sub.ID)
		c.publishLocked()
		close(call.done)
		c.pubMu.Unlock()
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
	if c.conn.Status().NodeRef.SubscriptionID == sub.ID {
		return c.apply(ctx, next, c.conn.Status().Tun)
	}
	return nil
}
