package core

import (
	"slices"

	"github.com/luynrs/justray/internal/daemon/store"
	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/engine"
	"github.com/luynrs/justray/internal/ipc"
)

func (c *Core) Snapshot() ipc.Snapshot {
	return clone(*c.snapshot.Load())
}

func (c *Core) Watch() (ipc.Snapshot, <-chan ipc.Snapshot, func()) {
	ch := make(chan ipc.Snapshot, 1)
	c.stMu.Lock()
	c.watchers[ch] = struct{}{}
	initial := c.snapshot.Load()
	c.stMu.Unlock()
	return clone(*initial), ch, func() {
		c.stMu.Lock()
		delete(c.watchers, ch)
		c.stMu.Unlock()
	}
}

func (c *Core) publish() {
	c.stMu.Lock()
	defer c.stMu.Unlock()
	c.publishLocked()
}

// Caller holds stMu.
func (c *Core) publishLocked() {
	state := c.state
	if c.changing != nil {
		state = c.changing.state
	}
	subs := make([]ipc.Subscription, len(state.Subscriptions))
	count := 0
	for i, sub := range state.Subscriptions {
		subs[i] = subView(sub, c.refreshes[sub.ID] != nil)
		count += len(sub.Nodes)
	}
	selected := state.Active
	if selected.NodeID == "" {
		selected = state.Last
	}
	snapshot := &ipc.Snapshot{
		Settings:      state.Settings.Clone(),
		Subscriptions: subs,
		Nodes:         c.nodes(state.Subscriptions, count),
		Status:        c.status(state),
		Selected:      selected,
		Collapsed:     slices.Clone(c.state.Collapsed),
	}
	c.snapshot.Store(snapshot)
	for ch := range c.watchers {
		select {
		case <-ch:
		default:
		}
		ch <- *snapshot
	}
}

func (c *Core) pruneProbes() {
	if len(c.probes) == 0 {
		return
	}
	keep := make(map[domain.NodeRef]engine.Result, len(c.probes))
	for _, node := range c.snapshot.Load().Nodes {
		if node.Probed {
			ref := node.Ref()
			keep[ref] = c.probes[ref]
		}
	}
	c.probes = keep
}

func (c *Core) status(state store.State) ipc.Status {
	status := c.conn.Status()
	if c.changing != nil {
		status = c.changing.before
		status.Busy = true
	}
	if !status.Connected {
		status.Port = state.Settings.Port
		status.Tun = state.Tun
	}
	return status
}

func (c *Core) nodes(subscriptions []store.Subscription, count int) []ipc.Node {
	out := make([]ipc.Node, 0, count)
	for _, subscription := range subscriptions {
		for _, node := range subscription.Nodes {
			ref := domain.NodeRef{SubscriptionID: subscription.ID, NodeID: node.ID}
			item := ipc.Node{
				NodeID:         node.ID,
				Name:           node.Name,
				Protocol:       node.ProtocolName(),
				Transport:      node.Transport.Network,
				Server:         node.Server,
				Port:           node.Port,
				SubscriptionID: subscription.ID,
				Probing:        c.probing != nil && c.probing.ctx.Err() == nil && c.probing.pending[ref],
			}
			if result, ok := c.probes[ref]; ok {
				item.Probed, item.Alive, item.Duration = true, result.Alive, result.Duration
				item.Failure, item.Error = result.Failure, result.Error
			}
			out = append(out, item)
		}
	}
	return out
}

func clone(snapshot ipc.Snapshot) ipc.Snapshot {
	snapshot.Settings = snapshot.Settings.Clone()
	snapshot.Subscriptions = slices.Clone(snapshot.Subscriptions)
	snapshot.Nodes = slices.Clone(snapshot.Nodes)
	snapshot.Collapsed = slices.Clone(snapshot.Collapsed)
	return snapshot
}

func subView(sub store.Subscription, refreshing bool) ipc.Subscription {
	return ipc.Subscription{
		SubscriptionID: sub.ID, Name: sub.Name, NodeCount: len(sub.Nodes),
		UpdatedAt: sub.UpdatedAt, Traffic: sub.Traffic,
		Refreshable: sub.URL != "", Refreshing: refreshing, Warning: sub.Warning,
	}
}
