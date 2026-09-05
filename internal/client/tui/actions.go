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
		m.collapsed[r.Sub.ID] = !m.collapsed[r.Sub.ID]
		m.clamp()
		return m, nil
	}
	if m.connecting {
		return m, nil
	}

	m.connecting = true
	act := m.client.Disconnect
	if !m.connected() || m.status.NodeRef != r.Node.Ref() {
		ref := r.Node.Ref()
		act = func() (ipc.Snapshot, error) { return m.client.Connect(ref) }
	}
	return m, snapshotCmd("connect", act)
}

func (m Model) collapse() (tea.Model, tea.Cmd) {
	r, ok := m.at()
	if !ok {
		return m, nil
	}
	m.collapsed[r.SubID()] = true
	if r.Kind == tree.Node {
		m.toHeader(r.SubID())
	}
	m.clamp()
	return m, nil
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
	if r, ok := m.at(); ok {
		m.collapsed[r.SubID()] = false
		m.clamp()
	}
	return m, nil
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
		return m, snapshotCmd("probe", func() (ipc.Snapshot, error) { return m.client.Probe(r.Node.Sub, r.Node.ID) })
	}
	if r.Sub.ID == tree.Default {
		return m, nil
	}
	return m, snapshotCmd("probe", func() (ipc.Snapshot, error) { return m.client.Probe(r.Sub.ID, "") })
}

func (m Model) probeAll() (tea.Model, tea.Cmd) {
	return m, snapshotCmd("probe", func() (ipc.Snapshot, error) { return m.client.Probe("", "") })
}

func (m Model) refresh() (tea.Model, tea.Cmd) {
	r, ok := m.at()
	if !ok {
		return m, nil
	}
	id := r.SubID()
	if id == tree.Default {
		return m, nil
	}
	return m, snapshotCmd("refresh", func() (ipc.Snapshot, error) { return m.client.Refresh(id) })
}

func (m Model) refreshAll() (tea.Model, tea.Cmd) {
	return m, snapshotCmd("refresh", m.client.RefreshAll)
}

func (m Model) moveSub(dir int) (tea.Model, tea.Cmd) {
	r, ok := m.at()
	if !ok || r.Kind != tree.Header || r.Sub.ID == tree.Default {
		return m, nil
	}
	id := r.Sub.ID
	i := slices.IndexFunc(m.subs, func(s ipc.Sub) bool { return s.ID == id })
	j := i + dir
	if i < 0 || j < 0 || j >= len(m.subs) {
		return m, nil
	}
	return m, snapshotCmd("mutation", func() (ipc.Snapshot, error) { return m.client.MoveSub(id, dir) })
}

func (m Model) setTun(enable bool) (tea.Model, tea.Cmd) {
	if m.connecting {
		return m, nil
	}
	m.connecting = true
	return m, snapshotCmd("connect", func() (ipc.Snapshot, error) { return m.client.SetTun(enable) })
}
