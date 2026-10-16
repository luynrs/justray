package core

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/luynrs/justray/internal/daemon/store"
	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/engine"
)

type probeCall struct {
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	target  domain.NodeRef
	pending map[domain.NodeRef]bool
	waiters int
	err     error
}

func (c *Core) Probe(ctx context.Context, sub, id string) error {
	ctx, cancel := c.requestContext(ctx)
	defer cancel()
	c.stMu.Lock()
	if err := ctx.Err(); err != nil {
		c.stMu.Unlock()
		return err
	}
	target := domain.NodeRef{SubscriptionID: sub, NodeID: id}
	for c.probing != nil && (c.probing.target != target || c.probing.ctx.Err() != nil) {
		if _, _, err := probeTargets(c.state.Subscriptions, sub, id); err != nil {
			c.stMu.Unlock()
			return err
		}
		c.probing.cancel()
		done := c.probing.done
		c.stMu.Unlock()
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
		c.stMu.Lock()
		if err := ctx.Err(); err != nil {
			c.stMu.Unlock()
			return err
		}
	}
	if c.changing != nil {
		c.stMu.Unlock()
		return errChanging
	}
	call := c.probing
	if call == nil {
		refs, nodes, err := probeTargets(c.state.Subscriptions, sub, id)
		if err != nil {
			c.stMu.Unlock()
			return err
		}
		jobCtx, cancel := context.WithTimeout(c.ctx, 30*time.Second)
		call = &probeCall{
			ctx: jobCtx, cancel: cancel, done: make(chan struct{}),
			target: target, pending: make(map[domain.NodeRef]bool, len(refs)),
		}
		for _, ref := range refs {
			call.pending[ref] = true
		}
		c.probing = call
		c.publishLocked()
		go c.runProbe(call, refs, nodes, c.state.Settings.Clone())
	}
	call.waiters++
	c.stMu.Unlock()
	defer func() {
		c.stMu.Lock()
		defer c.stMu.Unlock()
		call.waiters--
		if call.waiters == 0 && c.probing == call {
			call.cancel()
			c.publishLocked()
		}
	}()
	select {
	case <-call.done:
		return errors.Join(call.err, ctx.Err())
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Core) runProbe(call *probeCall, refs []domain.NodeRef, nodes []domain.Node, settings domain.Settings) {
	done := make(chan error, 1)
	changed := false
	go func() {
		done <- c.conn.Probe(call.ctx, nodes, settings, func(index int, result engine.Result) {
			c.stMu.Lock()
			defer c.stMu.Unlock()
			if call.ctx.Err() == nil {
				c.probes[refs[index]] = result
				delete(call.pending, refs[index])
				changed = true
			}
		})
	}()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			// inter. res
			c.stMu.Lock()
			if changed {
				c.publishLocked()
				changed = false
			}
			c.stMu.Unlock()
		case err := <-done:
			call.cancel()
			c.stMu.Lock()
			call.err = err
			c.probing = nil
			c.publishLocked()
			close(call.done)
			c.stMu.Unlock()
			return
		}
	}
}

func probeTargets(subscriptions []store.Subscription, subID, nodeID string) ([]domain.NodeRef, []domain.Node, error) {
	var refs []domain.NodeRef
	var nodes []domain.Node
	for _, sub := range subscriptions {
		if subID != "" && sub.ID != subID {
			continue
		}
		for _, node := range sub.Nodes {
			if nodeID != "" && node.ID != nodeID {
				continue
			}
			refs = append(refs, domain.NodeRef{SubscriptionID: sub.ID, NodeID: node.ID})
			nodes = append(nodes, node)
		}
	}
	if len(nodes) == 0 {
		return nil, nil, errors.New("nothing to probe")
	}
	if len(nodes) > 512 {
		return nil, nil, fmt.Errorf("too many nodes to probe: %d (maximum 512)", len(nodes))
	}
	return refs, nodes, nil
}
