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
	refs    []domain.NodeRef
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
	pending := make(map[string]*probeCall)
	var calls []*probeCall
	var targets []engine.Target
	var started []*probeCall
	c.stMu.Lock()
	for i, ref := range refs {
		call := c.probing[ref]
		if call == nil {
			call = pending[ref.NodeID]
			if call == nil {
				nodeCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
				call = &probeCall{ctx: nodeCtx, cancel: cancel, done: make(chan struct{})}
				pending[ref.NodeID] = call
				targets = append(targets, engine.Target{Context: nodeCtx, Node: nodes[i]})
				started = append(started, call)
			}
			call.refs = append(call.refs, ref)
			c.probing[ref] = call
		}
		call.waiters++
		calls = append(calls, call)
	}
	if len(pending) > 0 {
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
				for _, ref := range call.refs {
					if c.probing[ref] == call {
						delete(c.probing, ref)
						changed = true
					}
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
				for _, ref := range call.refs {
					c.probes[ref] = result
				}
			}
			call.cancel()
			// inter. res
			if c.probeTimer == nil {
				c.probeTimer = time.AfterFunc(100*time.Millisecond, func() {
					c.stMu.Lock()
					defer c.stMu.Unlock()
					var finished []*probeCall
					for _, call := range c.probing {
						if call.ctx.Err() == nil {
							continue
						}
						for _, ref := range call.refs {
							if c.probing[ref] == call {
								delete(c.probing, ref)
							}
						}
						finished = append(finished, call)
					}
					c.publishLocked()
					for _, call := range finished {
						close(call.done)
					}
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
