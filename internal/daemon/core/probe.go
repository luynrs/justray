package core

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/luynrs/justray/internal/daemon/store"
	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/engine"
)

func (c *Core) Probe(ctx context.Context, sub, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	state := c.current()
	refs, nodes, err := probeTargets(state.Subscriptions, sub, id)
	if err != nil {
		return err
	}

	c.stMu.Lock()
	var targets []domain.Node
	pending := make(map[string][]domain.NodeRef)
	for i, ref := range refs {
		if !c.probing[ref] {
			c.probing[ref] = true
			if len(pending[ref.NodeID]) == 0 {
				targets = append(targets, nodes[i])
			}
			pending[ref.NodeID] = append(pending[ref.NodeID], ref)
		}
	}
	c.stMu.Unlock()

	if len(targets) == 0 {
		return nil
	}
	c.publish()

	// inter. res
	var dirty atomic.Bool
	done, flushed := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(flushed)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if dirty.Swap(false) {
					c.publish()
				}
			}
		}
	}()

	defer func() {
		close(done)
		<-flushed
		c.stMu.Lock()
		for _, refs := range pending {
			for _, ref := range refs {
				delete(c.probing, ref)
			}
		}
		c.publishLocked()
		c.stMu.Unlock()
	}()

	onResult := func(nodeID string, res engine.Result) {
		c.stMu.Lock()
		for _, ref := range pending[nodeID] {
			c.probes[ref] = res
			delete(c.probing, ref)
		}
		delete(pending, nodeID)
		c.stMu.Unlock()

		dirty.Store(true)
	}

	return c.conn.Probe(ctx, targets, state.Settings, onResult)
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
