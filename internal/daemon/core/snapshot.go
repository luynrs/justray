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
	c.pubMu.Lock()
	c.watchers[ch] = struct{}{}
	initial := c.Snapshot()
	c.pubMu.Unlock()
	return initial, ch, func() {
		c.pubMu.Lock()
		delete(c.watchers, ch)
		c.pubMu.Unlock()
	}
}

func (c *Core) publish() {
	c.pubMu.Lock()
	defer c.pubMu.Unlock()
	c.publishLocked()
}

func (c *Core) publishLocked() {
	state := c.current()
	subs := make([]ipc.Sub, len(state.Subscriptions))
	for i, sub := range state.Subscriptions {
		subs[i] = subView(sub, c.refreshes[sub.ID] != nil)
	}
	selected := state.Active
	if selected.NodeID == "" {
		selected = state.Last
	}
	snapshot := &ipc.Snapshot{
		Settings:      cloneSettings(state.Settings),
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

func (c *Core) status(state store.PersistentState) ipc.Status {
	status := c.conn.Status()
	if !status.Connected {
		status.Port = state.Settings.Port
		status.Tun = state.Tun
	}
	return status
}

func (c *Core) nodes(subscriptions []store.Subscription) []ipc.Node {
	c.probeMu.Lock()
	defer c.probeMu.Unlock()

	live := map[domain.NodeRef]bool{}
	out := []ipc.Node{}
	for _, subscription := range subscriptions {
		for _, node := range subscription.Nodes {
			ref := domain.NodeRef{SubscriptionID: subscription.ID, NodeID: node.ID}
			live[ref] = true
			item := ipc.Node{
				ID:       node.ID,
				Name:     node.Name,
				Protocol: string(node.Protocol),
				Server:   node.Server,
				Port:     node.Port,
				Sub:      subscription.ID,
				Probing:  c.probing[ref],
			}
			if result, ok := c.probes[ref]; ok {
				item.Probed, item.Alive, item.MS = true, result.Alive, result.MS
			}
			out = append(out, item)
		}
	}
	for ref := range c.probes {
		if !live[ref] {
			delete(c.probes, ref)
		}
	}
	for ref := range c.probing {
		if !live[ref] {
			delete(c.probing, ref)
		}
	}
	return out
}

func cloneSnapshot(snapshot ipc.Snapshot) ipc.Snapshot {
	snapshot.Settings = cloneSettings(snapshot.Settings)
	snapshot.Subscriptions = slices.Clone(snapshot.Subscriptions)
	snapshot.Nodes = slices.Clone(snapshot.Nodes)
	snapshot.Collapsed = slices.Clone(snapshot.Collapsed)
	return snapshot
}

func subView(sub store.Subscription, refreshing bool) ipc.Sub {
	return ipc.Sub{
		ID: sub.ID, Name: sub.Name, Nodes: len(sub.Nodes),
		UpdatedAt: sub.UpdatedAt, Traffic: sub.Traffic,
		Refreshable: sub.URL != "", Refreshing: refreshing, Warning: sub.Warning,
	}
}

func cloneSettings(settings domain.Settings) domain.Settings {
	settings.Direct = slices.Clone(settings.Direct)
	settings.Proxy = slices.Clone(settings.Proxy)
	settings.Block = slices.Clone(settings.Block)
	return settings
}
