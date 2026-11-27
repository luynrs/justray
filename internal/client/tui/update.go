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

	"github.com/luynrs/justray/internal/client/tui/navigation"
	"github.com/luynrs/justray/internal/client/tui/settings"
	"github.com/luynrs/justray/internal/client/tui/style"
	"github.com/luynrs/justray/internal/client/tui/tree"
	"github.com/luynrs/justray/internal/ipc"
)

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		follow := m.visible()
		m.w, m.h = msg.Width, msg.Height
		style.ResizeInput(&m.filter, m.w-lipgloss.Width(m.filter.Prompt)-1)
		if m.dialog != nil {
			m.dialog.Resize(m.w, m.height())
		}
		m.resizeModal()
		if follow {
			m.cursor, m.scroll = m.list.Reveal(m.cursor, m.scroll, m.height())
		} else {
			m.cursor, m.scroll = m.list.Clamp(m.cursor, m.scroll, m.height())
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
			if msg.err != nil {
				if message := style.ErrorText(msg.err); message != "" && m.watch.Err() == nil {
					m.err, m.errAt = message, time.Now()
				}
				return m, nil
			}
			return m, watch(m.watch, m.client, m.updates)
		}
		return m, nil

	case tea.FocusMsg:
		m.focused = time.Now()
		return m, nil

	case pushed:
		return m.updateSnapshot(msg)
	}
	if key, ok := msg.(tea.KeyPressMsg); ok && key.String() == "ctrl+c" {
		return m, m.quit()
	}
	if m.saving {
		return m, nil
	}
	if _, ok := msg.(tea.MouseClickMsg); ok && time.Since(m.focused) < ipc.Retry {
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
		return m, m.updateModal(msg)
	}
	if m.dialog != nil {
		if mouse, ok := msg.(tea.MouseMsg); ok && !m.hasBody() {
			if _, wheel := msg.(tea.MouseWheelMsg); wheel || mouse.Mouse().Y != 0 {
				m.dialog.CancelNavigation()
				return m, nil
			}
		}
		closed, cmd := m.dialog.Update(msg)
		if closed {
			return m, m.closeSettings()
		}
		spinnerCommand := m.syncTTY()
		return m, tea.Batch(cmd, spinnerCommand)
	}
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m, m.key(msg)
	case tea.MouseMsg:
		return m, m.mouse(msg)
	default:
		if m.filter.Focused() {
			return m, m.updateFilter(msg)
		}
	}
	return m, nil
}

func (m *Model) updateSnapshot(message pushed) (tea.Model, tea.Cmd) {
	initial := m.snapshot.Settings.Port == 0
	selected, selectedOK := m.list.At(m.cursor)
	follow := m.visible()
	var spinnerCommand tea.Cmd
	if message.live {
		if !initial {
			for _, subscription := range message.snapshot.Subscriptions {
				if subscription.Warning != "" && !slices.ContainsFunc(m.snapshot.Subscriptions, func(previous ipc.Subscription) bool {
					return previous.SubscriptionID == subscription.SubscriptionID && previous.Warning == subscription.Warning && previous.UpdatedAt.Equal(subscription.UpdatedAt)
				}) {
					warnLine, _, _ := strings.Cut(subscription.Warning, "\n")
					m.err, m.errAt = "Warning: "+warnLine, time.Now()
				}
			}
		}
		if message.snapshot.Status.Error != "" && message.snapshot.Status.Error != m.snapshot.Status.Error {
			m.err, m.errAt = message.snapshot.Status.Error, time.Now()
		}
		m.snapshot = message.snapshot
		spinnerCommand = tea.Batch(m.syncTTY(), m.startSpinner())
	} else if errorText := style.ErrorText(message.err); errorText != "" && m.startup == nil && !m.busy {
		m.err, m.errAt = errorText, time.Now()
	}
	m.live = message.live
	m.list = m.data().List()
	switch {
	case initial && m.snapshot.Status.Connected:
		for index, rowIndex := range m.list.Selectable {
			if row := m.list.Rows[rowIndex]; row.Kind == tree.Node && row.Node.Ref() == m.snapshot.Status.NodeRef {
				m.cursor = index
				break
			}
		}
	case selectedOK:
		for index, rowIndex := range m.list.Selectable {
			row := m.list.Rows[rowIndex]
			if selected.Kind == tree.Node && row.Kind == tree.Header && row.Sub.SubscriptionID == selected.Sub.SubscriptionID {
				m.cursor = index
			}
			if row.Kind == selected.Kind && row.Sub.SubscriptionID == selected.Sub.SubscriptionID && (row.Kind != tree.Node || row.Node.Ref() == selected.Node.Ref()) {
				m.cursor = index
				break
			}
		}
	}
	if initial || follow {
		m.cursor, m.scroll = m.list.Reveal(m.cursor, m.scroll, m.height())
	} else {
		m.cursor, m.scroll = m.list.Clamp(m.cursor, m.scroll, m.height())
	}
	return m, tea.Batch(next(m.watch, m.updates), spinnerCommand)
}

func (m *Model) key(msg tea.KeyPressMsg) tea.Cmd {
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
			m.refilter()
			return nil
		}
		return m.updateFilter(msg)
	}
	if motion, handled := m.navigation.Read(k); handled {
		return m.navigate(motion)
	}

	switch k {
	case "left", "h":
		return m.fold(true)
	case "right", "l":
		return m.fold(false)
	case "enter":
		if r, ok := m.list.At(m.cursor); ok {
			return m.activate(r)
		}
	case "t":
		return m.probe()
	case "T", "shift+t":
		return action(false, m.start, func() error { return m.client.Probe(m.watch, "", "") })
	case "r":
		return m.refresh()
	case "R", "shift+r":
		return action(false, m.start, func() error { return m.client.RefreshSubscriptions(m.watch, "") })
	case "m":
		return m.setTun(!m.snapshot.Status.Tun)
	case "a":
		m.activeModal = modalAdd
		m.editor.SetValue("")
		m.resizeModal()
		return m.editor.Focus()
	case "o":
		if m.snapshot.Settings.Port == 0 {
			return nil
		}
		m.dialog = settings.New(m.snapshot.Settings, topLines, m.w, m.height())
		return m.syncTTY()
	case "/":
		m.filter.CursorEnd()
		return m.filter.Focus()
	case "d":
		if r, ok := m.list.At(m.cursor); ok && r.Removable() {
			m.deleteTarget = r
			m.activeModal = modalDelete
			m.resizeModal()
		}
	case "q":
		return m.quit()
	case "esc":
		if m.filter.Value() != "" {
			m.filter.SetValue("")
			m.refilter()
		}
	}
	return nil
}

func (m *Model) updateFilter(msg tea.Msg) tea.Cmd {
	query := m.filter.Value()
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(msg)
	if m.filter.Value() != query {
		m.refilter()
	}
	return cmd
}

func (m *Model) navigate(motion navigation.Motion) tea.Cmd {
	count := len(m.list.Selectable)
	if motion.Action == 0 || count == 0 {
		return nil
	}
	height, rows := m.height(), len(m.list.Rows)
	switch motion.Action {
	case navigation.Reorder:
		return m.reorder(motion.Direction)
	case navigation.Move:
		m.cursor += motion.Distance(height, count)
	case navigation.Page, navigation.HalfPage:
		delta := motion.Distance(height, rows)
		cursor, exact := slices.BinarySearch(m.list.Selectable, m.list.Selectable[m.cursor]+delta)
		if delta < 0 && !exact {
			cursor--
		}
		m.cursor, m.scroll = m.list.Reveal(cursor, m.scroll+delta, height)
		return nil
	case navigation.Scroll:
		m.cursor, m.scroll = m.list.Clamp(m.cursor, m.scroll+motion.Distance(height, rows), height)
		return nil
	case navigation.Line:
		m.cursor = motion.Count - 1
	case navigation.Last:
		m.cursor = count - 1
	}
	m.cursor, m.scroll = m.list.Reveal(m.cursor, m.scroll, height)
	return nil
}

func (m *Model) mouse(msg tea.MouseMsg) tea.Cmd {
	m.navigation.Reset()
	mouse := msg.Mouse()
	if click, ok := msg.(tea.MouseClickMsg); ok && click.Button == tea.MouseLeft && click.Y == 0 {
		if tun, ok := modeAt(click.X, m.w, lipgloss.Width(style.Title())); ok {
			return m.setTun(tun)
		}
		return nil
	}
	y := mouse.Y - topLines
	if !m.hasBody() || y < 0 || y >= m.height() {
		return nil
	}
	switch msg.(type) {
	case tea.MouseClickMsg:
		if mouse.Button != tea.MouseLeft {
			return nil
		}
		if cursor, ok := m.list.Point(m.scroll + y); ok {
			activate := cursor == m.cursor && !m.filter.Focused()
			m.cursor, m.scroll = m.list.Reveal(cursor, m.scroll, m.height())
			if activate {
				return m.activate(m.list.Rows[m.list.Selectable[cursor]])
			}
		}
	case tea.MouseWheelMsg:
		if m.filter.Focused() {
			return nil
		}
		switch mouse.Button {
		case tea.MouseWheelUp:
			m.scroll -= 3
		case tea.MouseWheelDown:
			m.scroll += 3
		default:
			return nil
		}
		m.cursor, m.scroll = m.list.Clamp(m.cursor, m.scroll, m.height())
	}
	return nil
}

// closeSettings saves on the way out
func (m *Model) closeSettings() tea.Cmd {
	next, previous, err := m.dialog.Result()
	switch {
	case err != nil:
		return nil
	case next.Equal(previous):
		m.dialog = nil
		return m.syncTTY()
	}
	m.saving = true
	m.err = ""
	return tea.Batch(m.syncTTY(), func() tea.Msg {
		return settingsSaved{err: request(m.start, func() error {
			return m.client.SetSettings(m.watch, previous, next)
		})}
	})
}
