package tui

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/luynrs/justray/internal/client/tui/tree"
	"github.com/luynrs/justray/internal/ipc"
)

func (m Model) activate(r tree.Row) (tea.Model, tea.Cmd) {
	if r.Kind == tree.Header {
		id := r.Sub.ID
		if m.client == nil {
			return m, nil
		}
		return m, m.actionCmd("collapse", nil, func() error { return m.client.SetCollapsed(m.watch, id, !slices.Contains(m.snapshot.Collapsed, id)) })
	}
	if m.busy {
		return m, nil
	}

	m.busy = true
	act := func() error { return m.client.Disconnect(m.watch) }
	if !m.connected() || m.snapshot.Status.NodeRef != r.Node.Ref() {
		ref := r.Node.Ref()
		act = func() error { return m.client.Connect(m.watch, ref, nil) }
	}
	return m, m.actionCmd("connection", m.start, act)
}

func (m Model) collapse() (tea.Model, tea.Cmd) {
	r, ok := m.at()
	if !ok {
		return m, nil
	}
	id := r.Sub.ID
	var cmd tea.Cmd
	if !slices.Contains(m.snapshot.Collapsed, id) {
		if m.client != nil {
			cmd = m.actionCmd("collapse", nil, func() error { return m.client.SetCollapsed(m.watch, id, true) })
		}
	}
	if r.Kind == tree.Node {
		m.toHeader(id)
	}
	return m, cmd
}

func (m *Model) toHeader(id string) {
	rows := m.rows()
	for i, idx := range tree.Selectable(rows) {
		if rows[idx].Kind == tree.Header && rows[idx].Sub.ID == id {
			m.cursor = i
			return
		}
	}
}

func (m Model) expand() (tea.Model, tea.Cmd) {
	r, ok := m.at()
	if !ok {
		return m, nil
	}
	id := r.Sub.ID
	var cmd tea.Cmd
	if slices.Contains(m.snapshot.Collapsed, id) {
		if m.client != nil {
			cmd = m.actionCmd("collapse", nil, func() error { return m.client.SetCollapsed(m.watch, id, false) })
		}
	}
	return m, cmd
}

func (m Model) probe() (tea.Model, tea.Cmd) {
	r, ok := m.at()
	if !ok {
		return m, nil
	}
	if r.Kind == tree.Node {
		if r.Node.Probing {
			return m, nil
		}
		return m, m.actionCmd("probe", m.start, func() error { return m.client.Probe(m.watch, r.Node.Sub, r.Node.ID) })
	}
	return m, m.actionCmd("probe", m.start, func() error { return m.client.Probe(m.watch, r.Sub.ID, "") })
}

func (m Model) probeAll() (tea.Model, tea.Cmd) {
	return m, m.actionCmd("probe", m.start, func() error { return m.client.Probe(m.watch, "", "") })
}

func (m Model) refresh() (tea.Model, tea.Cmd) {
	r, ok := m.at()
	if !ok {
		return m, nil
	}
	id := r.Sub.ID
	if !r.Sub.Refreshable {
		return m, nil
	}
	return m, m.actionCmd("refresh", m.start, func() error { return m.client.Refresh(m.watch, id) })
}

func (m Model) refreshAll() (tea.Model, tea.Cmd) {
	return m, m.actionCmd("refresh", m.start, func() error { return m.client.RefreshAll(m.watch) })
}

func (m Model) moveSub(dir int) (tea.Model, tea.Cmd) {
	r, ok := m.at()
	if !ok || r.Kind != tree.Header {
		return m, nil
	}
	id := r.Sub.ID
	i := slices.IndexFunc(m.snapshot.Subscriptions, func(sub ipc.Sub) bool { return sub.ID == id })
	j := i + dir
	if i < 0 || j < 0 || j >= len(m.snapshot.Subscriptions) {
		return m, nil
	}
	return m, m.actionCmd("mutation", m.start, func() error { return m.client.MoveSub(m.watch, id, dir) })
}

func (m Model) setTun(enable bool) (tea.Model, tea.Cmd) {
	if m.busy {
		return m, nil
	}
	op := "mode"
	if m.connected() {
		m.busy = true
		op = "connection"
	}
	return m, m.actionCmd(op, m.start, func() error { return m.client.SetTun(m.watch, enable) })
}
