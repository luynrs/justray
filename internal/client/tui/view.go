package tui

import (
	"cmp"
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
	proxyW, tunW := style.SegmentWidth(modeProxy), style.SegmentWidth(modeTun)
	start := style.RightStart(w, leftWidth, proxyW+tunW)
	return x >= start+proxyW, x >= start && x < w
}

func (m *Model) View() tea.View {
	v := tea.NewView(m.content())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.ReportFocus = true
	v.WindowTitle = "JustRay"
	return v
}

func (m *Model) content() string {
	if m.quitting || m.w <= 0 || m.h <= 0 {
		return ""
	}

	content := m.title()
	if m.hasBody() {
		var body string
		if m.dialog != nil {
			body = m.dialog.View()
		} else {
			body = m.tree()
		}
		content = style.Fit(content+"\n\n"+body, m.h-footerLines) + "\n\n" + m.footer() + "\n"
		err := m.err
		if m.dialog != nil {
			err = cmp.Or(m.dialog.Err(), err)
		}
		if m.dialog == nil && (m.filter.Focused() || m.filter.Value() != "" && err == "") {
			content += style.Clip(m.filter.View(), m.w)
		} else if err != "" {
			errStyle := style.Red
			if strings.HasPrefix(err, "Warning:") {
				errStyle = style.Yellow
			}
			content += style.Clip(errStyle.Bold(true).Italic(true).Render(style.Sanitize(err, true)), m.w)
		}
	}
	if m.activeModal != modalNone {
		return m.modal(content)
	}
	return content
}

func (m *Model) title() string {
	var right string
	if m.dialog != nil {
		right = m.dialog.TabBar()
	} else {
		right = style.Segment(modeProxy, !m.snapshot.Status.Tun) + style.Segment(modeTun, m.snapshot.Status.Tun)
	}
	return style.Clip(style.Flush(style.Title(), right, m.w), m.w)
}

func (m *Model) tree() string {
	data := m.data()
	h := m.height()
	rows := m.list.Rows

	lines := make([]string, 0, h)

	switch {
	case len(rows) > 0:
		cursor := m.list.Position(m.cursor)
		for i, r := range rows[m.scroll:min(m.scroll+h, len(rows))] {
			idx := m.scroll + i
			selected := idx == cursor || (r.Kind == tree.Meta && cursor >= 0 && idx == cursor+1 && rows[cursor].Kind == tree.Header)
			lines = append(lines, style.Clip(data.Render(r, selected, m.w), m.w))
		}
	case m.filter.Value() != "":
		lines = append(lines, style.Clip("    "+style.Gray.Render("No matches: "+m.filter.Value()), m.w))
	default:
		lines = append(lines, style.Clip("    "+style.Gray.Render("No subscriptions. Press ")+style.Bold.Render("a")+style.Gray.Render(" to add one"), m.w))
	}

	return strings.Join(lines, "\n")
}

func (m *Model) footer() string {
	busy := m.connecting || m.live && m.snapshot.Status.Busy
	var status string
	switch {
	case m.connected():
		icon, iconStyle := style.Dot(true), style.Green
		if busy {
			icon, iconStyle = m.spin.View(), style.Yellow
		}
		status = iconStyle.Render(icon) + " " + style.Sanitize(m.snapshot.Status.NodeName, m.emoji()) + " " + style.Gray.Render(style.Sep()) + " " + style.Uptime(m.snapshot.Status.Uptime())
	case m.startup != nil:
		status = style.Yellow.Render(m.spin.View()) + " " + style.Gray.Render("starting daemon")
	case busy:
		status = style.Yellow.Render(m.spin.View()) + " " + style.Gray.Render("connecting")
	default:
		status = style.Gray.Render(style.Dot(false) + " disconnected")
	}

	hint := m.footerHint()
	hintWidth := lipgloss.Width(hint)
	width := max(m.w-hintWidth-2, 0)
	status = ansi.Truncate(status, width, strings.Repeat(".", min(3, width)))
	return style.Pad(status, max(m.w-hintWidth, 0)) + hint
}

func (m *Model) footerHint() string {
	switch {
	case m.activeModal != modalNone:
		return style.KeyHints(m.w, [2]string{"esc", "cancel"})
	case m.editing():
		return style.KeyHints(m.w, [2]string{style.Enter(), "apply"}, [2]string{"esc", "cancel"})
	case m.dialog != nil && m.dialog.Changed():
		return style.KeyHints(m.w, [2]string{"esc", "save"}, [2]string{"q", "discard"}, [2]string{"?", "shortcuts"})
	}
	return m.helpHint()
}

func (m *Model) helpHint() string {
	return style.KeyHints(max(m.w-4, 1), [2]string{"?", "shortcuts"})
}
