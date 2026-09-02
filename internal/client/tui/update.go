package tui

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/luynrs/justray/internal/client/tui/settings"
	"github.com/luynrs/justray/internal/client/tui/tree"
	"github.com/luynrs/justray/internal/shared/rpc"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.editor.SetWidth(max(msg.Width-12, 10))
		m.clamp()

	case tea.KeyPressMsg:
		switch {
		case msg.String() == "ctrl+c":
			if m.settings != nil {
				m.settings = nil
				return m, tea.Quit
			}
			return m.quit()
		case m.settings != nil:
			return m.updateSettings(msg)
		}
		return m.key(msg)

	case tea.MouseMsg:
		if m.settings != nil {
			return m.updateSettings(msg)
		}
		return m.mouse(msg)

	case tea.PasteMsg:
		if m.editor.Focused() {
			var cmd tea.Cmd
			m.editor, cmd = m.editor.Update(msg)
			return m, cmd
		}

	case tick:
		return m, tickCmd()

	case spinner.TickMsg:
		if len(m.refreshing) == 0 && !m.connecting && m.live {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case loaded:
		m.err = ""
		if msg.op == "connect" {
			m.connecting = false
		}
		if msg.err != nil {
			m.err = msg.err.Error()
			if msg.op == "probe" {
				m.probing = nil
			}
			if msg.op == "refresh" {
				m.refreshing = nil
			}
			return m, nil
		}
		if msg.snapshot.Revision < m.revision {
			return m, nil
		}
		selected, selectedOK := m.at()
		m.revision = msg.snapshot.Revision
		m.subs, m.nodes = msg.snapshot.Subscriptions, msg.snapshot.Nodes
		m.status = msg.snapshot.Status
		m.since = time.Now().Add(-time.Duration(m.status.Uptime) * time.Second)
		m.emoji = msg.snapshot.Settings.Emoji == "on"
		m.live = true
		if msg.op == "probe" || msg.op == "sync" {
			m.probing = nil
		}
		if msg.op == "refresh" || msg.op == "sync" {
			m.refreshing = nil
		}
		if selectedOK && selected.Kind == tree.Header {
			m.toHeader(selected.Sub.ID)
		}
		m.clamp()
		if msg.op == "settings" {
			m.settings = settings.New(msg.snapshot.Settings, topLines)
		}

	case pushed:
		if msg.live {
			if msg.revision > m.revision || !m.live {
				return m, tea.Batch(next(m.statusCh), snapshotCmd("sync", m.client.Snapshot))
			}
			return m, next(m.statusCh)
		}
		m.live = false
		return m, tea.Batch(next(m.statusCh), m.spin.Tick)
	}
	return m, nil
}

func (m Model) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()

	switch {
	case m.confirmID != "":
		id := m.confirmID
		m.confirmQ, m.confirmID = "", ""
		if k == "y" {
			return m, snapshotCmd("mutation", func() (rpc.Snapshot, error) { return m.client.RemoveSub(id) })
		}
		return m, nil

	case m.editor.Focused():
		switch k {
		case "esc":
			m.editor.Blur()
			return m, nil
		case "enter":
			m.editor.Blur()
			url := strings.TrimSpace(m.editor.Value())
			if url == "" {
				return m, nil
			}
			return m, snapshotCmd("mutation", func() (rpc.Snapshot, error) { return m.client.AddSub(url) })
		}
		var cmd tea.Cmd
		m.editor, cmd = m.editor.Update(msg)
		return m, cmd

	case m.filter.Focused():
		return m.filterKey(msg)
	}

	switch k {
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
	case "shift+up":
		return m.moveSub(-1)
	case "shift+down":
		return m.moveSub(1)
	case "left", "h":
		return m.collapse()
	case "right", "l":
		return m.expand()
	case "enter":
		return m.activate()
	case "t":
		return m.probe()
	case "T":
		return m.probeAll()
	case "r":
		return m.refresh()
	case "R":
		return m.refreshAll()
	case "m":
		return m.setTun(!m.status.Tun)
	case "a":
		m.editor.SetValue("")
		return m, tea.Batch(m.editor.Focus(), textinput.Blink)
	case "o":
		return m, snapshotCmd("settings", m.client.Snapshot)
	case "/":
		return m, m.startFiltering()
	case "d":
		if r, ok := m.at(); ok {
			m.confirmQ, m.confirmID = "delete "+r.Sub.Name+"?", r.Sub.ID
		}
	case "q":
		return m.quit()
	case "esc":
		if m.filter.Value() != "" {
			m.filter.SetValue("")
			m.clamp()
		}
	}
	return m, nil
}

func (m Model) filterKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.filter.SetValue("")
		m.filter.Blur()
		m.clamp()
		return m, nil
	case "enter":
		m.filter.Blur()
		m.clamp()
		return m, nil
	}
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(msg)
	m.clamp()
	return m, cmd
}

func (m *Model) startFiltering() tea.Cmd {
	m.filter.CursorEnd()
	return tea.Batch(m.filter.Focus(), textinput.Blink)
}

func (m Model) mouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.editor.Focused() || m.confirmID != "" {
		return m, nil
	}
	mouse := msg.Mouse()
	switch msg.(type) {
	case tea.MouseClickMsg:
		if mouse.Button == tea.MouseLeft {
			return m.click(mouse.X, mouse.Y)
		}

	case tea.MouseWheelMsg:
		if m.filter.Focused() || (mouse.Button != tea.MouseWheelUp && mouse.Button != tea.MouseWheelDown) {
			return m, nil
		}
		if time.Since(m.wheel) < 20*time.Millisecond {
			return m, nil
		}
		m.wheel = time.Now()
		if mouse.Button == tea.MouseWheelUp {
			m.move(-1)
		} else {
			m.move(1)
		}
	}
	return m, nil
}

func (m Model) click(x, y int) (tea.Model, tea.Cmd) {
	if y == 0 {
		if tun, ok := modeAt(x, m.w); ok {
			return m.setTun(tun)
		}
		return m, nil
	}

	rows := m.rows()
	cursor, ok := tree.Point(rows, m.scroll, m.height(), topLines, y)
	if !ok {
		return m, nil
	}
	clicked := cursor == m.cursor
	m.cursor = cursor
	m.clamp()

	if r, _ := m.at(); clicked || r.Kind == tree.Header {
		return m.activate()
	}
	return m, nil
}

func (m Model) updateSettings(msg tea.Msg) (tea.Model, tea.Cmd) {
	closed, cmd := m.settings.Update(msg)
	if !closed {
		return m, cmd
	}
	return m.closeSettings()
}

// closeSettings saves on the way out
func (m Model) closeSettings() (Model, tea.Cmd) {
	next, changed, err := m.settings.Result()
	m.settings = nil
	switch {
	case err != nil:
		m.err = err.Error()
		return m, nil
	case !changed:
		return m, nil
	}
	m.emoji = next.Emoji == "on"
	m.connecting = true
	return m, tea.Batch(m.spin.Tick, snapshotCmd("connect", func() (rpc.Snapshot, error) { return m.client.SetSettings(next) }))
}
