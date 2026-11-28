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
	spinning, spinID := m.needsSpinner(), m.spin.ID()
	var cmd tea.Cmd
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
		cmd = tea.Batch(tea.Raw(string(query)), tea.RequestForegroundColor)

	case tea.ForegroundColorMsg:
		m.dimmedColors[nil] = lipgloss.Darken(msg.Color, 0.35)

	case uv.UnknownOscEvent:
		var index uint
		var value string
		if _, err := fmt.Sscanf(strings.TrimRight(string(msg), "\a\x1b\\"), "\x1b]4;%d;%s", &index, &value); err == nil && index < 16 {
			if parsed := ansi.XParseColor(value); parsed != nil {
				m.dimmedColors[ansi.BasicColor(index)] = lipgloss.Darken(parsed, 0.35)
			}
		}

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

	case tick:
		if m.err != "" && !m.filter.Focused() && time.Since(m.errAt) > 10*time.Second {
			m.err = ""
		}
		cmd = tickCmd()

	case spinner.TickMsg:
		if !spinning || msg.ID > 0 && msg.ID != spinID {
			return m, nil
		}
		m.spin, cmd = m.spin.Update(msg)

	case completed:
		if msg.connection {
			m.connecting = false
		}
		m.setErr(style.ErrorText(msg.err))

	case settingsSaved:
		m.saving = false
		if msg.err == nil {
			m.dialog = nil
		}
		m.setErr(style.ErrorText(msg.err))
		m.syncTTY()

	case started:
		if m.startup != nil {
			m.startup = nil
			if m.watch.Err() == nil {
				m.setErr(style.ErrorText(msg.err))
				cmd = watch(m.watch, m.client, m.updates)
			}
		}

	case tea.FocusMsg:
		m.focused = time.Now()

	case pushed:
		cmd = m.updateSnapshot(msg)

	default:
		cmd = m.updateInput(msg)
	}
	if (!spinning || m.spin.ID() != spinID) && m.needsSpinner() {
		cmd = tea.Batch(cmd, m.spin.Tick)
	}
	return m, cmd
}

func (m *Model) updateInput(msg tea.Msg) tea.Cmd {
	if key, ok := msg.(tea.KeyPressMsg); ok && key.String() == "ctrl+c" {
		return m.quit()
	}
	if m.saving {
		return nil
	}
	if _, ok := msg.(tea.MouseClickMsg); ok && time.Since(m.focused) < ipc.Retry {
		return nil
	}
	if m.activeModal != modalNone {
		return m.updateModal(msg)
	}
	if !m.editing() && m.openHelp(msg) {
		m.navigation.Reset()
		if m.dialog != nil {
			m.dialog.CancelNavigation()
		}
		m.activeModal = modalHelp
		m.resizeModal()
		m.help.GotoTop()
		return nil
	}
	if m.dialog != nil {
		return m.updateSettings(msg)
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
	return nil
}

func (m *Model) setErr(text string) {
	if text != "" {
		m.err, m.errAt = text, time.Now()
	}
}

func (m *Model) updateSnapshot(message pushed) tea.Cmd {
	if !message.live {
		if m.startup == nil && !m.connecting {
			m.setErr(style.ErrorText(message.err))
		}
		if !m.live {
			return next(m.watch, m.updates)
		}
	}
	initial := m.snapshot.Settings.Port == 0
	selected, selectedOK := m.list.At(m.cursor)
	follow := m.visible()
	m.live = message.live
	if message.live {
		if !initial {
			m.setErr(subWarning(m.snapshot.Subscriptions, message.snapshot.Subscriptions))
		}
		if message.snapshot.Status.Error != m.snapshot.Status.Error {
			m.setErr(message.snapshot.Status.Error)
		}
		m.snapshot = message.snapshot
		m.groups = tree.Groups(m.snapshot.Subscriptions, m.snapshot.Nodes)
		m.busy = m.snapshot.Status.Busy ||
			slices.ContainsFunc(m.snapshot.Subscriptions, func(sub ipc.Subscription) bool { return sub.Refreshing }) ||
			slices.ContainsFunc(m.snapshot.Nodes, func(node ipc.Node) bool { return node.Probing })
		m.syncTTY()
	}
	m.list = m.data().List(m.groups, m.filter.Value())
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
			if row.Sub.SubscriptionID != selected.Sub.SubscriptionID {
				continue
			}
			if row.Kind == tree.Header {
				m.cursor = index
			}
			if row.Kind == selected.Kind && (row.Kind == tree.Header || row.Node.Ref() == selected.Node.Ref()) {
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
	return next(m.watch, m.updates)
}

func subWarning(previous, current []ipc.Subscription) string {
	var old map[string]*ipc.Subscription
	for _, sub := range slices.Backward(current) {
		if sub.Warning == "" {
			continue
		}
		if old == nil {
			old = make(map[string]*ipc.Subscription, len(previous))
			for i := range previous {
				old[previous[i].SubscriptionID] = &previous[i]
			}
		}
		if prev := old[sub.SubscriptionID]; prev == nil || prev.Warning != sub.Warning || !prev.UpdatedAt.Equal(sub.UpdatedAt) {
			line, _, _ := strings.Cut(sub.Warning, "\n")
			return "Warning: " + line
		}
	}
	return ""
}

func (m *Model) key(msg tea.KeyPressMsg) tea.Cmd {
	k := msg.String()
	if k == "esc" && m.filter.Value() != "" {
		m.filter.SetValue("")
		m.refilter()
	}
	if m.filter.Focused() {
		if k != "esc" && k != "enter" {
			return m.updateFilter(msg)
		}
		m.filter.Blur()
		if m.err != "" {
			m.errAt = time.Now()
		}
		return nil
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
		m.syncTTY()
		return nil
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

func (m *Model) updateSettings(msg tea.Msg) tea.Cmd {
	if mouse, ok := msg.(tea.MouseMsg); ok && !m.hasBody() {
		if _, wheel := msg.(tea.MouseWheelMsg); wheel || mouse.Mouse().Y != 0 {
			m.dialog.CancelNavigation()
			return nil
		}
	}
	closed, cmd := m.dialog.Update(msg)
	if closed {
		return m.closeSettings()
	}
	m.syncTTY()
	return cmd
}

// closeSettings saves on the way out
func (m *Model) closeSettings() tea.Cmd {
	next, previous, err := m.dialog.Result()
	switch {
	case err != nil:
		return nil
	case next.Equal(previous):
		m.dialog = nil
		m.syncTTY()
		return nil
	}
	m.saving = true
	m.err = ""
	m.syncTTY()
	return func() tea.Msg {
		return settingsSaved{err: request(m.start, func() error {
			return m.client.SetSettings(m.watch, previous, next)
		})}
	}
}
