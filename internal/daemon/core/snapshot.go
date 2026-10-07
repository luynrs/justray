package core

import (
	"slices"

	"github.com/luynrs/justray/internal/daemon/store"
	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/ipc"
)

func (c *Core) Snapshot() ipc.Snapshot {
	return cloneSnapshot(*c.snapshot.Load())
}

func (c *Core) Watch() (ipc.Snapshot, <-chan ipc.Snapshot, func()) {
	ch := make(chan ipc.Snapshot, 1)
	c.stMu.Lock()
	c.watchers[ch] = struct{}{}
	initial := c.Snapshot()
	c.stMu.Unlock()
	return initial, ch, func() {
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
	subs := make([]ipc.Subscription, len(state.Subscriptions))
	for i, sub := range state.Subscriptions {
		subs[i] = subView(sub, c.refreshes[sub.ID] != nil)
	}
	selected := state.Active
	if selected.NodeID == "" {
		selected = state.Last
	}
	snapshot := &ipc.Snapshot{
		Settings:      state.Settings.Clone(),
		Subscriptions: subs,
		Nodes:         c.nodes(state.Subscriptions),
		Status:        c.status(state),
		Selected:      selected,
		Collapsed:     slices.Clone(state.Collapsed),
	}
	c.snapshot.Store(snapshot)
	for ch := range c.watchers {
		select {
		case ch <- *snapshot:
		default:
			select {
			case <-ch:
			default:
			}
			ch <- *snapshot
		}
	}
}

func (c *Core) status(state store.State) ipc.Status {
	status := c.conn.Status()
	if !status.Connected {
		status.Port = state.Settings.Port
		status.Tun = state.Tun
	}
	return status
}

// Caller holds stMu.
func (c *Core) nodes(subscriptions []store.Subscription) []ipc.Node {
	live := map[domain.NodeRef]bool{}
	out := []ipc.Node{}
	for _, subscription := range subscriptions {
		for _, node := range subscription.Nodes {
			ref := domain.NodeRef{SubscriptionID: subscription.ID, NodeID: node.ID}
			live[ref] = true
			item := ipc.Node{
				NodeID:         node.ID,
				Name:           node.Name,
				Protocol:       string(node.Protocol),
				Server:         node.Server,
				Port:           node.Port,
				SubscriptionID: subscription.ID,
				Probing:        c.probing[ref] != nil,
			}
			if result, ok := c.probes[ref]; ok {
				item.Probed, item.Alive, item.Duration = true, result.Alive, result.Duration
				item.Failure, item.Error = result.Failure, result.Error
			}
			out = append(out, item)
		}
	}
	for ref := range c.probes {
		if !live[ref] {
			delete(c.probes, ref)
		}
	}
	return out
}

func cloneSnapshot(snapshot ipc.Snapshot) ipc.Snapshot {
	snapshot.Settings = snapshot.Settings.Clone()
	snapshot.Subscriptions = slices.Clone(snapshot.Subscriptions)
	snapshot.Nodes = slices.Clone(snapshot.Nodes)
	snapshot.Collapsed = slices.Clone(snapshot.Collapsed)
	return snapshot
}

func subView(sub store.Subscription, refreshing bool) ipc.Subscription {
	return ipc.Subscription{
		SubscriptionID: sub.ID, Name: sub.Name, NodeCount: len(sub.Nodes),
		UpdatedAt: sub.UpdatedAt, Traffic: ipc.Traffic(sub.Traffic),
		Refreshable: sub.URL != "", Refreshing: refreshing, Warning: sub.Warning,
	}
}
