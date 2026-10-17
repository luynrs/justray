package tui

import (
	"errors"
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/luynrs/justray/internal/client/tui/tree"
	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/ipc"
)

func (m Model) activate(r tree.Row) (tea.Model, tea.Cmd) {
	if r.Kind == tree.Header {
		return m, action(false, nil, func() error { return m.client.SetCollapsed(m.watch, r.Sub.SubscriptionID, nil) })
	}
	if m.busy {
		return m, nil
	}
	m.err = ""

	m.busy = true
	spinnerCommand := m.startSpinner()
	act := func() error { return m.client.Disconnect(m.watch) }
	if !m.connected() || m.snapshot.Status.NodeRef != r.Node.Ref() {
		ref := r.Node.Ref()
		act = func() error { return m.awaitConnection(m.client.Connect(m.watch, ref, nil), ref, true) }
	}
	return m, tea.Batch(spinnerCommand, action(true, m.start, act))
}

func (m Model) collapse() (tea.Model, tea.Cmd) {
	row, ok := m.at()
	if !ok {
		return m, nil
	}
	if row.Kind == tree.Node {
		rows := m.rows()
		for i, index := range tree.Selectable(rows) {
			if rows[index].Kind == tree.Header && rows[index].Sub.SubscriptionID == row.Sub.SubscriptionID {
				m.cursor, m.scroll = tree.Reveal(rows, i, m.scroll, m.height())
				break
			}
		}
	}
	return m, action(false, nil, func() error { return m.client.SetCollapsed(m.watch, row.Sub.SubscriptionID, new(true)) })
}

func (m Model) expand() (tea.Model, tea.Cmd) {
	r, ok := m.at()
	if !ok {
		return m, nil
	}
	return m, action(false, nil, func() error { return m.client.SetCollapsed(m.watch, r.Sub.SubscriptionID, new(false)) })
}

func (m Model) probe() (tea.Model, tea.Cmd) {
	row, ok := m.at()
	if !ok || row.Node.Probing {
		return m, nil
	}
	return m, action(false, m.start, func() error { return m.client.Probe(m.watch, row.Sub.SubscriptionID, row.Node.NodeID) })
}

func (m Model) refresh() (tea.Model, tea.Cmd) {
	row, ok := m.at()
	if !ok || !row.Sub.Refreshable {
		return m, nil
	}
	return m, action(false, m.start, func() error { return m.client.RefreshSubscriptions(m.watch, row.Sub.SubscriptionID) })
}

func (m Model) moveSub(dir int) (tea.Model, tea.Cmd) {
	r, ok := m.at()
	if !ok || r.Kind != tree.Header {
		return m, nil
	}
	id := r.Sub.SubscriptionID
	i := slices.IndexFunc(m.snapshot.Subscriptions, func(sub ipc.Subscription) bool { return sub.SubscriptionID == id })
	j := i + dir
	if i < 0 || j < 0 || j >= len(m.snapshot.Subscriptions) {
		return m, nil
	}
	return m, action(false, m.start, func() error { return m.client.MoveSubscription(m.watch, id, dir) })
}

func (m Model) setTun(enable bool) (tea.Model, tea.Cmd) {
	if m.busy {
		return m, nil
	}
	m.err = ""
	m.busy = m.connected()
	spinnerCommand := m.startSpinner()
	return m, tea.Batch(spinnerCommand, action(m.busy, m.start, func() error {
		return m.awaitConnection(m.client.SetTun(m.watch, enable), m.snapshot.Selected, enable)
	}))
}

func (m Model) awaitConnection(err error, ref domain.NodeRef, tun bool) error {
	if errors.Is(err, ipc.ErrElevate) {
		_, err = m.client.AwaitConnection(m.watch, ref, &tun)
	}
	return err
}
