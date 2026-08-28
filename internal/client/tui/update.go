package tui

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/luynrs/justray/internal/client/tui/settings"
	"github.com/luynrs/justray/internal/client/tui/tree"
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
				next, cmd := m.closeSettings()
				return next, tea.Sequence(cmd, tea.Quit)
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
		if m.editing {
			var cmd tea.Cmd
			m.editor, cmd = m.editor.Update(msg)
			return m, cmd
		}

	case settingsLoaded:
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		s, err := msg.s.Normalize()
		if err != nil {
			m.err = err.Error()
			return m, nil
		}
		m.emoji = s.Emoji == "on"
		if msg.open {
			m.settings = settings.New(s, topLines)
		}
		return m, nil

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
		if msg.err != nil {
			m.err = msg.err.Error()
			m.probing, m.refreshing = nil, nil
		}
		if msg.subs != nil {
			m.subs, m.refreshing = msg.subs, nil
		}
		if msg.nodes != nil {
			m.nodes, m.probing = msg.nodes, nil
		}
		m.clamp()

	case connectResult:
		m.connecting = false
		m.err = ""
		if msg.err != nil {
			m.err = msg.err.Error()
		}

	case pushed:
		m.live = msg.live
		if msg.live {
			m.since = time.Now().Add(-time.Duration(msg.st.Uptime) * time.Second)
			m.status, m.err = msg.st, ""
			return m, next(m.statusCh)
		}
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
		yes := k == "y"
		if yes {
			return m, act(m.client, func() error { return m.client.RemoveSub(id) })
		}
		return m, nil

	case m.editing:
		switch k {
		case "esc":
			m.editing = false
			m.editor.Blur()
			return m, nil
		case "enter":
			m.editing = false
			m.editor.Blur()
			url := strings.TrimSpace(m.editor.Value())
			if url == "" {
				return m, nil
			}
			return m, act(m.client, func() error { _, err := m.client.AddSub(url); return err })
		}
		var cmd tea.Cmd
		m.editor, cmd = m.editor.Update(msg)
		return m, cmd

	case m.filtering:
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
		m.editing = true
		m.editor.SetValue("")
		return m, tea.Batch(m.editor.Focus(), textinput.Blink)
	case "o":
		return m, settingsCmd(m.client, true)
	case "/":
		return m, m.startFiltering()
	case "d":
		if r, ok := m.at(); ok {
			name := tree.Data{Subs: m.subs}.SubName(r.SubID())
			m.confirmQ, m.confirmID = "delete "+name+"?", r.SubID()
		}
	case "q":
		return m.quit()
	case "esc":
		if m.query != "" {
			m.query = ""
			m.clamp()
		}
	}
	return m, nil
}

func (m Model) filterKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.filtering, m.query = false, ""
		m.filter.Blur()
		m.clamp()
		return m, nil
	case "enter":
		m.filtering = false
		m.filter.Blur()
		m.clamp()
		return m, nil
	}
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(msg)
	m.query = m.filter.Value()
	m.clamp()
	return m, cmd
}

func (m *Model) startFiltering() tea.Cmd {
	m.filtering = true
	m.filter.SetValue(m.query)
	m.filter.CursorEnd()
	return tea.Batch(m.filter.Focus(), textinput.Blink)
}

func (m Model) mouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.editing || m.confirmID != "" {
		return m, nil
	}
	mouse := msg.Mouse()
	switch msg.(type) {
	case tea.MouseClickMsg:
		if mouse.Button == tea.MouseLeft {
			return m.click(mouse.X, mouse.Y)
		}

	case tea.MouseWheelMsg:
		if m.filtering || (mouse.Button != tea.MouseWheelUp && mouse.Button != tea.MouseWheelDown) {
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
	return m, tea.Batch(m.spin.Tick, connectCmd(func() error { _, err := m.client.SetSettings(next); return err }))
}
