package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/luynrs/justray/internal/client/tui/style"
	"github.com/luynrs/justray/internal/client/tui/tree"
	"github.com/luynrs/justray/internal/shared/version"
)

const (
	modeProxy = " Proxy "
	modeTun   = "  TUN  "
)

func modeAt(x, w int) (tun, ok bool) {
	proxyW, tunW := segW(modeProxy), segW(modeTun)
	switch x -= w - proxyW - tunW; {
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
	switch {
	case m.quitting, m.w == 0:
		return ""
	case m.settings != nil:
		body := m.titleLine() + "\n\n" + m.settings.View(m.w, max(m.h-topLines-1, 1))
		return style.Fit(body, m.h-1) + "\n" + m.clip(style.Indent(m.hints(m.w-2)))
	case m.h < topLines+footerLines+1:
		return m.titleLine()
	}

	body := m.tree()
	if m.editing {
		body = m.titleLine() + "\n\n" + m.editor.View()
	}
	return style.Fit(body, m.h-footerLines) + "\n" + m.footer()
}

func (m Model) titleLine() string {
	left := style.Title.Render("JustRay") + " " + style.Dim.Render(version.String())
	if m.settings == nil && (m.filtering || m.query != "") {
		left += " " + style.Dim.Render("~ Search:") + " " + m.filter.View()
	}

	var right string
	if m.settings == nil {
		right = style.Segment(modeProxy, !m.status.Tun) + style.Segment(modeTun, m.status.Tun)
	}
	return m.clip(style.Flush(left, right, m.w))
}

func (m Model) tree() string {
	data := m.data()
	rows := data.Rows()
	h := m.height()

	lines := make([]string, 0, topLines+h)
	lines = append(lines, m.titleLine(), "")

	switch {
	case len(rows) > 0:
		cursor := -1
		if sel := tree.Selectable(rows); m.cursor < len(sel) {
			cursor = sel[m.cursor]
		}
		for i, r := range rows[m.scroll:min(m.scroll+h, len(rows))] {
			lines = append(lines, m.clip(data.Render(r, m.scroll+i == cursor, m.w)))
		}
	case m.query != "":
		lines = append(lines, m.clip("    "+style.Dim.Render(fmt.Sprintf("No matches for %q", m.query))))
	default:
		lines = append(lines, m.clip("    "+style.Dim.Render("No subscriptions yet.")))
	}

	return strings.Join(lines, "\n")
}

func (m Model) keys() [][2]string {
	switch {
	case m.settings != nil:
		return m.settings.Hints()
	case m.confirmID != "":
		return [][2]string{{"y", "Delete"}, {"any", "Cancel"}}
	case m.editing:
		return [][2]string{{"↵", "Add"}, {"esc", "Cancel"}}
	}
	return [][2]string{
		{"↑/↓", "Move"}, {"←/→", "Fold"}, {"↵", "Toggle"}, {"t", "Ping"}, {"r", "Refresh"},
		{"m", "Mode"}, {"/", "Filter"}, {"a", "Add"}, {"d", "Delete"}, {"o", "Settings"}, {"q", "Quit"},
	}
}

func (m Model) hints(maxW int) string {
	out, w := "", 0
	for _, k := range m.keys() {
		hint := style.Strong.Render(k[0]) + " " + style.Dim.Render(k[1])
		if out != "" {
			hint = "  " + hint
		}
		if w += lipgloss.Width(hint); w > maxW {
			break
		}
		out += hint
	}
	return out
}

func (m Model) footer() string {
	icon := "○"
	if m.connected() {
		icon = "●"
	}
	if m.connecting {
		icon = m.spin.View()
	}

	var status string
	switch {
	case m.connected():
		status = style.Strong.Render(fmt.Sprintf("%s %s · %s", icon, style.Sanitize(m.status.NodeName, m.emoji), style.Uptime(time.Since(m.since))))
	case m.live:
		status = style.Dim.Render(icon + " disconnected")
	default:
		status = style.Dim.Render(m.spin.View() + " connecting")
	}
	if m.err != "" {
		status += "   " + style.Err.Render(style.Sanitize(style.FirstLine(m.err), true))
	}

	hints := m.hints(m.w)
	if m.confirmID != "" {
		q := style.Err.Render(style.Sanitize(m.confirmQ, true))
		hints = q + "  " + m.hints(max(m.w-lipgloss.Width(q)-2, 0))
	}

	return "\n" + m.clip(status) + "\n" + m.clip(hints)
}

func (m Model) clip(s string) string { return style.Clip(s, m.w) }
