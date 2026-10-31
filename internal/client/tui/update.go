package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/luynrs/justray/internal/client/tui/settings"
	"github.com/luynrs/justray/internal/client/tui/style"
	"github.com/luynrs/justray/internal/client/tui/tree"
	"github.com/luynrs/justray/internal/ipc"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.ColorProfileMsg:
		clear(m.dimmedColors)
		if msg.Profile < colorprofile.ANSI256 {
			return m, nil
		}
		var query []byte
		for index := range 16 {
			query = fmt.Appendf(query, "\x1b]4;%d;?\x1b\\", index)
		}
		return m, tea.Batch(tea.Raw(string(query)), tea.RequestForegroundColor)

	case tea.ForegroundColorMsg:
		m.dimmedColors[nil] = lipgloss.Darken(msg.Color, 0.35)
		return m, nil

	case uv.UnknownOscEvent:
		var index uint
		var value string
		if _, err := fmt.Sscanf(strings.TrimRight(string(msg), "\a\x1b\\"), "\x1b]4;%d;%s", &index, &value); err == nil && index < 16 {
			if parsed := ansi.XParseColor(value); parsed != nil {
				m.dimmedColors[ansi.BasicColor(index)] = lipgloss.Darken(parsed, 0.35)
			}
		}
		return m, nil

	case tea.WindowSizeMsg:
		rows := m.rows()
		followSelection := m.selectionVisible(rows)
		m.w, m.h = msg.Width, msg.Height
		style.ResizeInput(&m.filter, m.w-lipgloss.Width(m.filter.Prompt)-1)
		if m.dialog != nil {
			m.dialog.Resize(m.w, m.height())
		}
		m.resizeModal()
		if followSelection {
			m.cursor, m.scroll = tree.Reveal(rows, m.cursor, m.scroll, m.height())
		} else {
			m.cursor, m.scroll = tree.Clamp(rows, m.cursor, m.scroll, m.height())
		}
		return m, nil

	case tick:
		if m.err != "" && !m.filter.Focused() && time.Since(m.errAt) > 10*time.Second {
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
		if message := style.ErrorText(msg.err); message != "" {
			m.err, m.errAt = message, time.Now()
		}
		return m, nil

	case settingsSaved:
		m.saving = false
		if msg.err == nil {
			m.dialog = nil
		} else if message := style.ErrorText(msg.err); message != "" {
			m.err, m.errAt = message, time.Now()
		}
		return m, m.syncTTY()

	case started:
		if m.startup != nil {
			m.startup = nil
			if message := style.ErrorText(msg.err); message != "" && m.watch.Err() == nil {
				m.err, m.errAt = message, time.Now()
			}
		}
		return m, nil

	case pushed:
		return m.updateSnapshot(msg)
	}
	if key, ok := msg.(tea.KeyPressMsg); ok && key.String() == "ctrl+c" {
		return m.quit()
	}
	if m.saving {
		return m, nil
	}
	showHelp := false
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		showHelp = m.activeModal == modalNone && msg.String() == "?" && !m.editing()
	case tea.MouseClickMsg:
		showHelp = m.activeModal == modalNone && !m.editing() && msg.Button == tea.MouseLeft && m.h >= topLines+footerLines+1 && msg.Y == m.h-2 && msg.X >= m.w-lipgloss.Width(m.helpHint()) && msg.X < m.w && strings.HasSuffix(m.footerHint(), m.helpHint())
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
		if mouse, ok := msg.(tea.MouseMsg); ok && m.h < topLines+footerLines+1 {
			if _, wheel := msg.(tea.MouseWheelMsg); wheel || mouse.Mouse().Y != 0 {
				m.dialog.CancelNavigation()
				return m, nil
			}
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

func (model Model) updateSnapshot(message pushed) (tea.Model, tea.Cmd) {
	initial := model.snapshot.Settings.Port == 0
	rows := model.rows()
	selected, selectedOK := tree.At(rows, model.cursor)
	followSelection := model.selectionVisible(rows)
	var spinnerCommand tea.Cmd
	if message.live {
		if !initial {
			for _, subscription := range message.snapshot.Subscriptions {
				if subscription.Warning != "" && !slices.ContainsFunc(model.snapshot.Subscriptions, func(previous ipc.Subscription) bool {
					return previous.SubscriptionID == subscription.SubscriptionID && previous.Warning == subscription.Warning && previous.UpdatedAt.Equal(subscription.UpdatedAt)
				}) {
					model.err, model.errAt = subscription.Warning, time.Now()
				}
			}
		}
		if message.snapshot.Status.Error != "" && message.snapshot.Status.Error != model.snapshot.Status.Error {
			model.err, model.errAt = message.snapshot.Status.Error, time.Now()
		}
		model.snapshot = message.snapshot
		spinnerCommand = tea.Batch(model.syncTTY(), model.startSpinner())
	} else if errorText := style.ErrorText(message.err); errorText != "" && model.startup == nil && !model.busy {
		model.err, model.errAt = errorText, time.Now()
	}
	model.live = message.live
	rows = model.rows()
	switch {
	case initial && model.snapshot.Status.Connected:
		for index, rowIndex := range tree.Selectable(rows) {
			if row := rows[rowIndex]; row.Kind == tree.Node && row.Node.Ref() == model.snapshot.Status.NodeRef {
				model.cursor = index
				break
			}
		}
	case selectedOK:
		for index, rowIndex := range tree.Selectable(rows) {
			row := rows[rowIndex]
			if selected.Kind == tree.Node && row.Kind == tree.Header && row.Sub.SubscriptionID == selected.Sub.SubscriptionID {
				model.cursor = index
			}
			if row.Kind == selected.Kind && row.Sub.SubscriptionID == selected.Sub.SubscriptionID && (row.Kind != tree.Node || row.Node.Ref() == selected.Node.Ref()) {
				model.cursor = index
				break
			}
		}
	}
	if initial || followSelection {
		model.cursor, model.scroll = tree.Reveal(rows, model.cursor, model.scroll, model.height())
	} else {
		model.cursor, model.scroll = tree.Clamp(rows, model.cursor, model.scroll, model.height())
	}
	return model, tea.Batch(next(model.watch, model.updates), spinnerCommand)
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
			if m.err != "" {
				m.errAt = time.Now()
			}
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
		return m, action(false, m.start, func() error { return m.client.Probe(m.watch, "", "") })
	case "r":
		return m.refresh()
	case "R", "shift+r":
		return m, action(false, m.start, func() error { return m.client.RefreshSubscriptions(m.watch, "") })
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
		m.dialog = settings.New(m.snapshot.Settings, topLines, m.w, m.height())
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
		if tun, ok := modeAt(x, m.w, lipgloss.Width(style.Title())); ok && x < m.w {
			return m.setTun(tun)
		}
		return m, nil
	}
	if m.h < topLines+footerLines+1 {
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

	if r, _ := tree.At(rows, m.cursor); clicked {
		return m.activate(r)
	}
	return m, nil
}

// closeSettings saves on the way out
func (m Model) closeSettings() (Model, tea.Cmd) {
	next, previous, err := m.dialog.Result()
	switch {
	case err != nil:
		return m, nil
	case next.Equal(previous):
		m.dialog = nil
		return m, m.syncTTY()
	}
	m.saving = true
	m.err = ""
	return m, tea.Batch(m.syncTTY(), func() tea.Msg {
		return settingsSaved{err: request(m.start, func() error {
			return m.client.SetSettings(m.watch, previous, next)
		})}
	})
}
