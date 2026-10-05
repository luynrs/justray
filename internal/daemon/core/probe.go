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
	ref     domain.NodeRef
	waiters int
	err     error
}

func (c *Core) Probe(ctx context.Context, sub, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	state := c.current()
	refs, nodes, err := probeTargets(state.Subscriptions, sub, id)
	if err != nil {
		return err
	}
	if len(nodes) > 512 {
		return fmt.Errorf("too many nodes to probe: %d (maximum 512)", len(nodes))
	}
	var calls []*probeCall
	var targets []engine.Target
	var started []*probeCall
	c.stMu.Lock()
	for i, ref := range refs {
		call := c.probing[ref]
		if call == nil {
			nodeCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
			call = &probeCall{ctx: nodeCtx, cancel: cancel, done: make(chan struct{}), ref: ref}
			targets = append(targets, engine.Target{Context: nodeCtx, Node: nodes[i]})
			started = append(started, call)
			c.probing[ref] = call
		}
		call.waiters++
		calls = append(calls, call)
	}
	if len(started) > 0 {
		c.publishLocked()
	}
	c.stMu.Unlock()
	defer func() {
		c.stMu.Lock()
		defer c.stMu.Unlock()
		changed := false
		for _, call := range calls {
			call.waiters--
			if call.waiters == 0 {
				call.cancel()
				if c.probing[call.ref] == call {
					delete(c.probing, call.ref)
					changed = true
				}
			}
		}
		if changed {
			c.publishLocked()
		}
	}()
	if len(targets) > 0 {
		go c.conn.Probe(targets, state.Settings, func(index int, result engine.Result, err error) {
			call := started[index]
			c.stMu.Lock()
			defer c.stMu.Unlock()
			if call.ctx.Err() != nil {
				return
			}
			call.err = err
			if err == nil {
				c.probes[call.ref] = result
			}
			call.cancel()
			// inter. res
			if c.probeTimer == nil {
				c.probeTimer = time.AfterFunc(100*time.Millisecond, func() {
					c.stMu.Lock()
					defer c.stMu.Unlock()
					for ref, call := range c.probing {
						if call.ctx.Err() == nil {
							continue
						}
						delete(c.probing, ref)
						close(call.done)
					}
					c.publishLocked()
					c.probeTimer = nil
				})
			}
		})
	}
	var result error
	for _, call := range calls {
		select {
		case <-call.done:
			result = errors.Join(result, call.err)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return errors.Join(result, ctx.Err())
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
		return nil, nil, fmt.Errorf("nothing to probe")
	}
	return refs, nodes, nil
}
