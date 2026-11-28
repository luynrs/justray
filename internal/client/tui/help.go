package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/luynrs/justray/internal/client/tui/style"
)

func (m *Model) openHelp(msg tea.Msg) bool {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return msg.String() == "?"
	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft || !m.hasBody() || msg.Y != m.h-2 || msg.X >= m.w {
			return false
		}
		hint := m.helpHint()
		return msg.X >= m.w-lipgloss.Width(hint) && strings.HasSuffix(m.footerHint(), hint)
	}
	return false
}

func shortcuts(width int) string {
	keys := [][2]string{
		{style.Move(), "Move"}, {style.Fold(), "Fold"}, {style.Enter(), "Toggle"}, {"m", "Proxy / TUN"},
		{"a/d", "Add / Delete"}, {"t/T", "Ping one/all"}, {"r/R", "Refresh one/all"},
		{"o", "Settings"}, {"/", "Filter"}, {"q", "Quit"},
	}
	lines := make([]string, len(keys))
	columnWidth := 0
	for i, key := range keys {
		lines[i] = style.KeyHint(style.Pad(key[0], 6), key[1])
		columnWidth = max(columnWidth, lipgloss.Width(lines[i]))
	}
	if width >= columnWidth*2+2 {
		rows := (len(lines) + 1) / 2
		for i := range rows {
			if i+rows < len(lines) {
				lines[i] = style.Pad(lines[i], width/2) + lines[i+rows]
			}
		}
		lines = lines[:rows]
	}
	return ansi.Wrap(strings.Join(lines, "\n"), width, "")
}
