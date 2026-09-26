package tui

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/luynrs/justray/internal/client/tui/tree"
	"github.com/luynrs/justray/internal/ipc"
)

func (m Model) activate() (tea.Model, tea.Cmd) {
	r, ok := m.at()
	if !ok {
		return m, nil
	}
	if r.Kind == tree.Header {
		id := r.Sub.ID
		target := !m.collapsed[id]
		m.collapsed[id] = target
		m.clamp()
		if m.client == nil {
			return m, nil
		}
		return m, actionCmd("collapse", nil, func() error { return m.client.SetCollapsed(id, target) })
	}
	if m.busy {
		return m, nil
	}

	m.busy = true
	act := m.client.Disconnect
	if !m.connected() || m.snapshot.Status.NodeRef != r.Node.Ref() {
		ref := r.Node.Ref()
		act = func() error { return m.client.Connect(ref) }
	}
	return m, actionCmd("connection", m.start, act)
}

func (m Model) collapse() (tea.Model, tea.Cmd) {
	r, ok := m.at()
	if !ok {
		return m, nil
	}
	id := r.Sub.ID
	var cmd tea.Cmd
	if !m.collapsed[id] {
		m.collapsed[id] = true
		if m.client != nil {
			cmd = actionCmd("collapse", nil, func() error { return m.client.SetCollapsed(id, true) })
		}
	}
	if r.Kind == tree.Node {
		m.toHeader(id)
	}
	m.clamp()
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
	if m.collapsed[id] {
		m.collapsed[id] = false
		if m.client != nil {
			cmd = actionCmd("collapse", nil, func() error { return m.client.SetCollapsed(id, false) })
		}
	}
	m.clamp()
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
		return m, actionCmd("probe", m.start, func() error { return m.client.Probe(r.Node.Sub, r.Node.ID) })
	}
	return m, actionCmd("probe", m.start, func() error { return m.client.Probe(r.Sub.ID, "") })
}

func (m Model) probeAll() (tea.Model, tea.Cmd) {
	return m, actionCmd("probe", m.start, func() error { return m.client.Probe("", "") })
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
	return m, actionCmd("refresh", m.start, func() error { return m.client.Refresh(id) })
}

func (m Model) refreshAll() (tea.Model, tea.Cmd) {
	return m, actionCmd("refresh", m.start, m.client.RefreshAll)
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
	return m, actionCmd("mutation", m.start, func() error { return m.client.MoveSub(id, dir) })
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
	return m, actionCmd(op, m.start, func() error { return m.client.SetTun(enable) })
}
