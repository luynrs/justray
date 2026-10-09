package tui

import (
	"fmt"
	"slices"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/luynrs/justray/internal/client/tui/settings"
	"github.com/luynrs/justray/internal/client/tui/tree"
	"github.com/luynrs/justray/internal/ipc"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.resizeModal()
		m.clamp()
		return m, nil

	case tick:
		if m.err != "" && time.Since(m.errAt) > 10*time.Second {
			m.err = ""
		}
		return m, tickCmd()

	case spinner.TickMsg:
		if !m.needsSpinner() {
			m.spinnerActive = false
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case completed:
		if msg.connection {
			m.busy = false
		}
		if msg.err != nil {
			m.err, m.errAt = msg.err.Error(), time.Now()
		}
		return m, nil

	case restored:
		if m.restore != nil {
			m.restore = nil
			if msg.err != nil && m.watch.Err() == nil {
				m.err, m.errAt = "Could not restore connection: "+msg.err.Error(), time.Now()
			}
		}
		return m, nil

	case pushed:
		if !msg.live {
			m.live = false
			if msg.err != nil && m.restore == nil && !m.busy {
				m.err, m.errAt = msg.err.Error(), time.Now()
			}
			return m, next(m.watch, m.updates)
		}
		initial := m.snapshot.Settings.Port == 0
		selected, selectedOK := m.at()
		if !initial {
			for _, sub := range msg.snapshot.Subscriptions {
				if sub.Warning != "" && !slices.ContainsFunc(m.snapshot.Subscriptions, func(previous ipc.Subscription) bool {
					return previous.SubscriptionID == sub.SubscriptionID && previous.Warning == sub.Warning && previous.UpdatedAt.Equal(sub.UpdatedAt)
				}) {
					m.err, m.errAt = sub.Warning, time.Now()
				}
			}
		}
		m.snapshot = msg.snapshot
		spinnerCommand := m.syncTTY()
		m.live = true
		spinnerCommand = tea.Batch(spinnerCommand, m.startSpinner())
		rows := m.rows()
		switch {
		case initial && m.snapshot.Status.Connected:
			for i, idx := range tree.Selectable(rows) {
				if r := rows[idx]; r.Kind == tree.Node && r.Node.Ref() == m.snapshot.Status.NodeRef {
					m.cursor = i
					break
				}
			}
		case selectedOK:
			for i, idx := range tree.Selectable(rows) {
				row := rows[idx]
				if selected.Kind == tree.Node && row.Kind == tree.Header && row.Sub.SubscriptionID == selected.Sub.SubscriptionID {
					m.cursor = i
				}
				if row.Kind == selected.Kind && row.Sub.SubscriptionID == selected.Sub.SubscriptionID && (row.Kind != tree.Node || row.Node.Ref() == selected.Node.Ref()) {
					m.cursor = i
					break
				}
			}
		}
		if initial {
			m.cursor, m.scroll = tree.Reveal(rows, m.cursor, m.scroll, m.height())
		} else {
			m.cursor, m.scroll = tree.Clamp(rows, m.cursor, m.scroll, m.height())
		}
		return m, tea.Batch(next(m.watch, m.updates), spinnerCommand)
	}
	if key, ok := msg.(tea.KeyPressMsg); ok && key.String() == "ctrl+c" {
		return m.quit()
	}
	showHelp := false
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		showHelp = m.activeModal == modalNone && msg.String() == "?" && !m.editing()
	case tea.MouseClickMsg:
		showHelp = m.activeModal == modalNone && !m.editing() && msg.Button == tea.MouseLeft && m.h >= topLines+footerLines+1 && msg.Y == m.h-1 && msg.X >= m.w-lipgloss.Width(m.helpHint()) && msg.X < m.w
	}
	if showHelp {
		m.navigation.Reset()
		if m.dialog != nil {
			m.dialog.CancelNavigation()
		}
		m.activeModal = modalHelp
		m.resizeModal()
		m.help.GotoTop()
		return m, nil
	}
	if m.activeModal != modalNone {
		return m.updateModal(msg)
	}
	if m.dialog != nil {
		if _, wheel := msg.(tea.MouseWheelMsg); wheel && m.h < topLines+footerLines+1 {
			m.dialog.CancelNavigation()
			return m, nil
		}
		closed, cmd := m.dialog.Update(msg)
		if closed {
			return m.closeSettings()
		}
		spinnerCommand := m.syncTTY()
		return m, tea.Batch(cmd, spinnerCommand)
	}
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.key(msg)
	case tea.MouseMsg:
		return m.mouse(msg)
	default:
		if m.filter.Focused() {
			return m.updateFilter(msg)
		}
	}
	return m, nil
}

func (m Model) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()

	if m.filter.Focused() {
		switch k {
		case "esc", "enter":
			if k == "esc" {
				m.filter.SetValue("")
			}
			m.filter.Blur()
			m.clamp()
			return m, nil
		}
		return m.updateFilter(msg)
	}
	if motion, handled := m.navigation.Read(k); handled {
		m.navigate(motion)
		return m, nil
	}

	switch k {
	case "shift+up":
		return m.moveSub(-1)
	case "shift+down":
		return m.moveSub(1)
	case "left", "h":
		return m.collapse()
	case "right", "l":
		return m.expand()
	case "enter":
		if r, ok := m.at(); ok {
			return m.activate(r)
		}
	case "t":
		return m.probe()
	case "T", "shift+t":
		return m, action(false, m.start(false), func() error { return m.client.Probe(m.watch, "", "") })
	case "r":
		return m.refresh()
	case "R", "shift+r":
		return m, action(false, m.start(false), func() error { return m.client.RefreshSubscriptions(m.watch) })
	case "m":
		return m.setTun(!m.snapshot.Status.Tun)
	case "a":
		m.activeModal = modalAdd
		m.editor.SetValue("")
		m.resizeModal()
		return m, m.editor.Focus()
	case "o":
		if m.snapshot.Settings.Port == 0 {
			return m, nil
		}
		m.dialog = settings.New(m.snapshot.Settings, topLines)
		spinnerCommand := m.syncTTY()
		return m, spinnerCommand
	case "/":
		m.filter.CursorEnd()
		return m, m.filter.Focus()
	case "d":
		if r, ok := m.at(); ok && r.Removable() {
			m.deleteTarget = r
			m.activeModal = modalDelete
			m.resizeModal()
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

func (m Model) updateFilter(msg tea.Msg) (tea.Model, tea.Cmd) {
	query := m.filter.Value()
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(msg)
	if m.filter.Value() != query {
		m.clamp()
	}
	return m, cmd
}

func (m Model) mouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	m.navigation.Reset()
	mouse := msg.Mouse()
	switch msg.(type) {
	case tea.MouseClickMsg:
		if mouse.Button == tea.MouseLeft {
			return m.click(mouse.X, mouse.Y)
		}

	case tea.MouseWheelMsg:
		if m.filter.Focused() || m.h < topLines+footerLines+1 || mouse.Y < topLines || mouse.Y >= topLines+m.height() ||
			(mouse.Button != tea.MouseWheelUp && mouse.Button != tea.MouseWheelDown) {
			return m, nil
		}
		delta := 3
		if mouse.Button == tea.MouseWheelUp {
			delta = -3
		}
		m.cursor, m.scroll = tree.Clamp(m.rows(), m.cursor, m.scroll+delta, m.height())
	}
	return m, nil
}

func (m Model) click(x, y int) (tea.Model, tea.Cmd) {
	if y == 0 {
		if tun, ok := modeAt(x, m.w, lipgloss.Width(m.titleLeft())); ok && x < m.w {
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
	m.cursor, m.scroll = tree.Reveal(rows, m.cursor, m.scroll, m.height())

	if r, _ := tree.At(rows, m.cursor); clicked || r.Kind == tree.Header {
		return m.activate(r)
	}
	return m, nil
}

// closeSettings saves on the way out
func (m Model) closeSettings() (Model, tea.Cmd) {
	next, changed, err := m.dialog.Result()
	m.dialog = nil
	spinnerCommand := m.syncTTY()
	switch {
	case err != nil:
		m.err, m.errAt = err.Error(), time.Now()
		return m, spinnerCommand
	case !changed:
		return m, spinnerCommand
	}
	old := m.snapshot.Settings
	otherSettings := next
	otherSettings.Autostart = old.Autostart
	return m, tea.Batch(spinnerCommand, action(false, m.start(false), func() error {
		if next.Autostart != old.Autostart {
			if err := m.client.SetAutostart(m.watch, next.Autostart == "on"); err != nil {
				return err
			}
		}
		if otherSettings.Equal(old) {
			return nil
		}
		err := m.client.SetSettings(m.watch, next)
		if err != nil && next.Autostart != old.Autostart {
			return fmt.Errorf("autostart changed; settings: %w", err)
		}
		return err
	}))
}
