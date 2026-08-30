package tui

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/luynrs/justray/internal/client/tui/tree"
	"github.com/luynrs/justray/internal/shared/domain"
	"github.com/luynrs/justray/internal/shared/rpc"
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
		act = func() (rpc.Status, error) { return m.client.Connect(ref) }
	}
	return m, tea.Batch(m.spin.Tick, connectCmd(func() error { _, err := act(); return err }))
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
	m.probing = map[domain.NodeRef]bool{}
	if r.Kind == tree.Node {
		m.probing[r.Node.Ref()] = true
		return m, probeCmd(m.client, r.Node.Sub, r.Node.ID)
	}
	for _, n := range m.data().SubNodes(r.Sub.ID) {
		m.probing[n.Ref()] = true
	}
	return m, probeCmd(m.client, r.Sub.ID, "")
}

func (m Model) probeAll() (tea.Model, tea.Cmd) {
	m.probing = map[domain.NodeRef]bool{}
	for _, n := range m.nodes {
		m.probing[n.Ref()] = true
	}
	return m, probeCmd(m.client, "", "")
}

func (m Model) refresh() (tea.Model, tea.Cmd) {
	r, ok := m.at()
	if !ok {
		return m, nil
	}
	id := r.SubID()
	m.refreshing = map[string]bool{id: true}
	return m, tea.Batch(m.spin.Tick, act(m.client, func() error { _, err := m.client.Refresh(id); return err }))
}

func (m Model) refreshAll() (tea.Model, tea.Cmd) {
	m.refreshing = map[string]bool{}
	for _, sub := range m.subs {
		m.refreshing[sub.ID] = true
	}
	return m, tea.Batch(m.spin.Tick, act(m.client, func() error { _, err := m.client.RefreshAll(); return err }))
}

func (m Model) moveSub(dir int) (tea.Model, tea.Cmd) {
	r, ok := m.at()
	if !ok || r.Kind != tree.Header {
		return m, nil
	}
	id := r.Sub.ID
	i := slices.IndexFunc(m.subs, func(s rpc.Sub) bool { return s.ID == id })
	j := i + dir
	if i < 0 || j < 0 || j >= len(m.subs) {
		return m, nil
	}
	subs := slices.Clone(m.subs)
	subs[i], subs[j] = subs[j], subs[i]
	m.subs = subs
	m.toHeader(id)
	m.clamp()
	return m, act(m.client, func() error { return m.client.MoveSub(id, dir) })
}

func (m Model) setTun(enable bool) (tea.Model, tea.Cmd) {
	if m.connecting {
		return m, nil
	}
	m.connecting = true
	return m, tea.Batch(m.spin.Tick, connectCmd(func() error { _, err := m.client.SetTun(enable); return err }))
}
