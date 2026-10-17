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
	v.WindowTitle = "JustRay"
	return v
}

func (m Model) content() string {
	if m.quitting || m.w <= 0 || m.h <= 0 {
		return ""
	}

	content := m.titleLine()
	if m.h >= topLines+footerLines+1 {
		var body string
		var rows []tree.Row
		if m.dialog != nil {
			body = m.dialog.View()
		} else {
			rows = m.rows()
			body = m.tree(rows)
		}
		content = style.Fit(content+"\n\n"+body, m.h-footerLines) + "\n\n" + m.footer(rows)
	}
	if m.activeModal != modalNone {
		return m.modalView(content)
	}
	return content
}

func (m Model) titleLeft() string {
	if m.dialog == nil && (m.filter.Focused() || m.filter.Value() != "") {
		return m.filter.View()
	}
	return style.Title()
}

func (m Model) titleLine() string {
	var right string
	if m.dialog != nil {
		right = m.dialog.TabBar()
	} else {
		right = style.Segment(modeProxy, !m.snapshot.Status.Tun) + style.Segment(modeTun, m.snapshot.Status.Tun)
	}
	return m.clip(style.Flush(m.titleLeft(), right, m.w))
}

func (m Model) tree(rows []tree.Row) string {
	data := m.data()
	h := m.height()

	lines := make([]string, 0, h)

	switch {
	case len(rows) > 0:
		cursor := -1
		selection := 0
		for i, row := range rows {
			if row.Selectable() {
				if selection == m.cursor {
					cursor = i
					break
				}
				selection++
			}
		}
		for i, r := range rows[m.scroll:min(m.scroll+h, len(rows))] {
			idx := m.scroll + i
			selected := idx == cursor || (r.Kind == tree.Meta && cursor >= 0 && idx == cursor+1 && rows[cursor].Kind == tree.Header)
			lines = append(lines, m.clip(data.Render(r, selected, m.w)))
		}
	case m.filter.Value() != "":
		lines = append(lines, m.clip("    "+style.Gray.Render(fmt.Sprintf("No matches for %q", m.filter.Value()))))
	default:
		lines = append(lines, m.clip("    "+style.Gray.Render("No subscriptions yet.")))
	}

	return strings.Join(lines, "\n")
}

func (m Model) footer(rows []tree.Row) string {
	icon := style.Dot(false)
	if m.connected() {
		icon = style.Dot(true)
	}
	if m.busy || m.snapshot.Status.Busy || m.startup != nil {
		icon = m.spin.View()
	}

	var status string
	switch {
	case m.connected():
		iconStyle := style.Green
		if m.busy || m.snapshot.Status.Busy {
			iconStyle = style.Yellow
		}
		status = iconStyle.Render(icon) + " " + style.Sanitize(m.snapshot.Status.NodeName, m.emoji()) + " " + style.Gray.Render(style.Sep()) + " " + style.Uptime(m.snapshot.Status.Uptime())
	case m.startup != nil:
		status = style.Yellow.Render(icon) + " " + style.Gray.Render("starting daemon")
	case m.busy:
		status = style.Yellow.Render(icon) + " " + style.Gray.Render("connecting")
	default:
		status = style.Gray.Render(icon) + " " + style.Gray.Render("disconnected")
	}
	err := m.err
	if m.dialog != nil {
		err = cmp.Or(m.dialog.Err(), err)
	}
	if err != "" {
		status += "  " + style.Red.Render(style.Sanitize(err, true))
	} else if r, ok := tree.At(rows, m.cursor); m.dialog == nil && ok && r.Sub.Warning != "" {
		warnLine, _, _ := strings.Cut(r.Sub.Warning, "\n")
		status += "   " + style.Yellow.Render(style.Sanitize(warnLine, true))
	}

	hint := m.footerHint()
	hintWidth := lipgloss.Width(hint)
	width := max(m.w-hintWidth-2, 0)
	status = ansi.Truncate(status, width, strings.Repeat(".", min(3, width)))
	return style.Pad(status, max(m.w-hintWidth, 0)) + hint
}

func (m Model) footerHint() string {
	switch {
	case m.activeModal != modalNone:
		return style.KeyHints(m.w, [2]string{"esc", "cancel"})
	case m.editing():
		return style.KeyHints(m.w, [2]string{style.Enter(), "apply"}, [2]string{"esc", "cancel"})
	case m.dialog != nil:
		return style.KeyHints(m.w, [2]string{"esc", "save"}, [2]string{"q", "quit"}, [2]string{"?", "shortcuts"})
	}
	return m.helpHint()
}

func (m Model) helpHint() string {
	return style.KeyHints(max(m.w-4, 1), [2]string{"?", "shortcuts"})
}

func (m Model) clip(s string) string { return style.Clip(s, m.w) }
