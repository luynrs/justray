package tui

import (
	"cmp"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/luynrs/justray/internal/client/tui/style"
	"github.com/luynrs/justray/internal/client/tui/tree"
	"github.com/luynrs/justray/internal/version"
)

const (
	modeProxy = " Proxy "
	modeTun   = "  TUN  "
)

func modeAt(x, w, leftWidth int) (tun, ok bool) {
	proxyW, tunW := segW(modeProxy), segW(modeTun)
	switch x -= max(w-proxyW-tunW, leftWidth+2); {
	case x < 0:
		return false, false
	case x < proxyW:
		return false, true
	}
	return true, true
}

func segW(s string) int { return lipgloss.Width(style.Segment(s, false)) }

func (m Model) View() tea.View {
	v := tea.NewView(m.content())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m Model) content() string {
	if m.quitting || m.w <= 0 || m.h <= 0 {
		return ""
	}

	content := m.titleLine()
	if m.h >= topLines+footerLines+1 {
		var body string
		if m.dialog != nil {
			body = m.dialog.View(m.w, m.height())
		} else {
			body = m.tree()
		}
		content = style.Fit(content+"\n\n"+body, m.h-footerLines) + "\n\n" + m.footer()
	}
	if m.activeModal != modalNone {
		return m.modalView(content)
	}
	return content
}

func (m Model) titleLeft() string {
	left := style.Title.Render("JustRay") + " " + style.Dim.Render(version.String())
	if m.dialog != nil {
		left += "  " + m.dialog.TabBar(max(m.w-lipgloss.Width(left)-2, 10))
	} else if m.filter.Focused() || m.filter.Value() != "" {
		left += " " + style.Dim.Render("~ Search:") + " " + m.filter.View()
	}
	return left
}

func (m Model) titleLine() string {
	var right string
	if m.dialog == nil {
		right = style.Segment(modeProxy, !m.snapshot.Status.Tun) + style.Segment(modeTun, m.snapshot.Status.Tun)
	}
	return m.clip(style.Flush(m.titleLeft(), right, m.w))
}

func (m Model) tree() string {
	data := m.data()
	rows := data.Rows()
	h := m.height()

	lines := make([]string, 0, h)

	switch {
	case len(rows) > 0:
		cursor := -1
		if sel := tree.Selectable(rows); m.cursor < len(sel) {
			cursor = sel[m.cursor]
		}
		for i, r := range rows[m.scroll:min(m.scroll+h, len(rows))] {
			idx := m.scroll + i
			selected := idx == cursor || (r.Kind == tree.Meta && cursor >= 0 && idx == cursor+1 && rows[cursor].Kind == tree.Header)
			lines = append(lines, m.clip(data.Render(r, selected, m.w)))
		}
	case m.filter.Value() != "":
		lines = append(lines, m.clip("    "+style.Dim.Render(fmt.Sprintf("No matches for %q", m.filter.Value()))))
	default:
		lines = append(lines, m.clip("    "+style.Dim.Render("No subscriptions yet.")))
	}

	return strings.Join(lines, "\n")
}

func (m Model) shortcuts() string {
	var keys [][2]string
	switch {
	case m.dialog != nil:
		keys = m.dialog.Hints()
	case m.filter.Focused():
		keys = [][2]string{{"enter", "Apply"}, {"esc", "Cancel"}}
	default:
		keys = [][2]string{
			{style.Move(), "Move"}, {style.Fold(), "Fold"}, {"t/T", "Ping"}, {"r/R", "Refresh"}, {"enter", "Toggle"},
			{"a/d", "Add / Delete"}, {"m", "Mode"}, {"/", "Filter"}, {"o", "Settings"}, {"q", "Quit"},
		}
	}
	lines := make([]string, len(keys))
	columnWidth := 0
	for i, key := range keys {
		lines[i] = style.Strong.Render(style.Pad(key[0], 6)) + " " + key[1]
		columnWidth = max(columnWidth, lipgloss.Width(lines[i]))
	}
	if width := m.help.Width(); width >= columnWidth*2+2 {
		rows := (len(lines) + 1) / 2
		for i := range rows {
			if i+rows < len(lines) {
				lines[i] = style.Pad(lines[i], width/2) + lines[i+rows]
			}
		}
		lines = lines[:rows]
	}
	return strings.Join(lines, "\n")
}

func (m Model) footer() string {
	icon := style.Dot(false)
	if m.connected() {
		icon = style.Dot(true)
	}
	if m.busy || m.restore != nil {
		icon = m.spin.View()
	}

	var status string
	switch {
	case m.connected():
		iconStyle := style.Alive
		if m.busy {
			iconStyle = style.Pending
		}
		status = iconStyle.Render(icon) + " " + style.Sanitize(m.snapshot.Status.NodeName, m.emoji()) + " " + style.Dim.Render(style.Sep()) + " " + style.Uptime(m.snapshot.Status.Uptime())
	case m.restore != nil:
		status = style.Pending.Render(icon) + " " + style.Dim.Render("restoring connection")
	case m.busy:
		status = style.Pending.Render(icon) + " " + style.Dim.Render("connecting")
	default:
		status = style.Dim.Render(icon) + " " + style.Dim.Render("disconnected")
	}
	err := m.err
	if m.dialog != nil {
		err = cmp.Or(m.dialog.Err(), err)
	}
	if err != "" {
		status += "  " + style.Dead.Render(style.Sanitize(err, true))
	} else if r, ok := m.at(); m.dialog == nil && ok && r.Sub.Warning != "" {
		warnLine, _, _ := strings.Cut(r.Sub.Warning, "\n")
		status += "   " + style.Pending.Render(style.Sanitize(warnLine, true))
	}

	hint := m.helpHint()
	width := max(m.w-len(hint)-2, 0)
	status = ansi.Truncate(status, width, strings.Repeat(".", min(3, width)))
	return style.Pad(status, max(m.w-len(hint), 0)) + style.Dim.Render(hint)
}

func (m Model) helpHint() string {
	key := "?"
	if m.editing() {
		key = "f1"
	}
	if m.w < len(key) {
		return ""
	}
	if m.w < len(key+" for help")+4 {
		return key
	}
	return key + " for help"
}

func (m Model) clip(s string) string { return style.Clip(s, m.w) }
