package tui

import (
	"errors"
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/luynrs/justray/internal/client/tui/tree"
	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/ipc"
)

func (m *Model) activate(r tree.Row) tea.Cmd {
	if r.Kind == tree.Header {
		return action(false, nil, func() error { return m.client.SetCollapsed(m.watch, r.Sub.SubscriptionID, nil) })
	}
	if m.connecting {
		return nil
	}
	m.err = ""

	m.connecting = true
	act := func() error { return m.client.Disconnect(m.watch) }
	if !m.connected() || m.snapshot.Status.NodeRef != r.Node.Ref() {
		ref := r.Node.Ref()
		act = func() error { return m.await(m.client.Connect(m.watch, ref, nil), ref, true) }
	}
	return action(true, m.start, act)
}

func (m *Model) fold(collapsed bool) tea.Cmd {
	row, ok := m.list.At(m.cursor)
	if !ok {
		return nil
	}
	if collapsed && row.Kind == tree.Node {
		for m.list.Rows[m.list.Selectable[m.cursor]].Kind == tree.Node {
			m.cursor--
		}
		m.cursor, m.scroll = m.list.Reveal(m.cursor, m.scroll, m.height())
	}
	return action(false, nil, func() error { return m.client.SetCollapsed(m.watch, row.Sub.SubscriptionID, &collapsed) })
}

func (m *Model) probe() tea.Cmd {
	row, ok := m.list.At(m.cursor)
	if !ok {
		return nil
	}
	var nodeID string
	if row.Node != nil {
		if row.Node.Probing {
			return nil
		}
		nodeID = row.Node.NodeID
	}
	return action(false, m.start, func() error { return m.client.Probe(m.watch, row.Sub.SubscriptionID, nodeID) })
}

func (m *Model) refresh() tea.Cmd {
	row, ok := m.list.At(m.cursor)
	if !ok || !row.Sub.Refreshable {
		return nil
	}
	return action(false, m.start, func() error { return m.client.RefreshSubscriptions(m.watch, row.Sub.SubscriptionID) })
}

func (m *Model) reorder(dir int) tea.Cmd {
	r, ok := m.list.At(m.cursor)
	if !ok || r.Kind != tree.Header {
		return nil
	}
	id := r.Sub.SubscriptionID
	i := slices.IndexFunc(m.snapshot.Subscriptions, func(sub ipc.Subscription) bool { return sub.SubscriptionID == id })
	j := i + dir
	if i < 0 || j < 0 || j >= len(m.snapshot.Subscriptions) {
		return nil
	}
	return action(false, m.start, func() error { return m.client.MoveSubscription(m.watch, id, dir) })
}

func (m *Model) setTun(enable bool) tea.Cmd {
	if m.connecting {
		return nil
	}
	m.err = ""
	m.connecting = m.connected()
	selected := m.snapshot.Selected
	return action(m.connecting, m.start, func() error {
		return m.await(m.client.SetTun(m.watch, enable), selected, enable)
	})
}

func (m *Model) await(err error, ref domain.NodeRef, tun bool) error {
	if errors.Is(err, ipc.ErrElevate) {
		_, err = m.client.AwaitConnection(m.watch, ref, &tun)
	}
	return err
}
