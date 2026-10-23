package core

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/sagernet/sing-box/log"

	"github.com/luynrs/justray/internal/daemon/store"
	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/ipc"
)

type refreshCall struct {
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	waiters int
	err     error
}

func (c *Core) RefreshSubscriptions(ctx context.Context, ids ...string) error {
	ctx, cancel := c.requestContext(ctx)
	defer cancel()
	if err := c.ctx.Err(); err != nil {
		return err
	}
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

func (c *Core) refresh(ctx context.Context, sub store.Subscription) error {
	var call *refreshCall
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		c.stMu.Lock()
		call = c.refreshes[sub.ID]
		if call != nil && call.ctx.Err() != nil {
			c.stMu.Unlock()
			select {
			case <-call.done:
			case <-ctx.Done():
				return ctx.Err()
			}
			continue
		}
		if call == nil {
			index := slices.IndexFunc(c.state.Subscriptions, func(current store.Subscription) bool { return current.ID == sub.ID })
			if index < 0 {
				c.stMu.Unlock()
				return fmt.Errorf("subscription %q not found", sub.ID)
			}
			sub = c.state.Subscriptions[index]
			jobCtx, cancel := context.WithTimeout(c.ctx, ipc.Timeout)
			call = &refreshCall{ctx: jobCtx, cancel: cancel, done: make(chan struct{})}
			c.refreshes[sub.ID] = call
			c.publishLocked()
			go c.refreshSubscription(call, sub)
		}
		call.waiters++
		c.stMu.Unlock()
		break
	}
	defer func() {
		c.stMu.Lock()
		call.waiters--
		if call.waiters == 0 {
			call.cancel()
		}
		c.stMu.Unlock()
	}()
	select {
	case <-call.done:
		return call.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Core) refreshSubscription(call *refreshCall, sub store.Subscription) {
	var err error
	defer func() {
		if err != nil && call.ctx.Err() != nil {
			err = call.ctx.Err()
		}
		if err != nil {
			log.Error("cannot refresh subscription ", sub.ID, ": ", err)
		}
		call.cancel()
		c.stMu.Lock()
		call.err = err
		delete(c.refreshes, sub.ID)
		c.publishLocked()
		close(call.done)
		c.stMu.Unlock()
	}()
	sub, err = c.subs.Refresh(call.ctx, sub)
	if err != nil {
		return
	}
	change, err := c.startChange(call.ctx, true, func(ctx context.Context) error {
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
			return c.external(func() error { return c.conn.Disconnect(ctx) })
		}
		if status := c.conn.Status(); status.Connected && status.NodeRef.SubscriptionID == sub.ID {
			node, ref, err := find(next.Subscriptions, status.NodeRef)
			if err != nil {
				return err
			}
			return c.external(func() error { return c.conn.Apply(ctx, node, ref, next.Settings, status.Tun) })
		}
		return nil
	})
	if err == nil {
		<-change.done
		err = change.err
	}
}
