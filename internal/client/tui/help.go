package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/luynrs/justray/internal/client/tui/style"
)

func (m Model) shortcuts(width int) string {
	keys := [][2]string{{style.Move(), "Move"}}
	if m.dialog != nil {
		keys = append(keys, [][2]string{
			{"tab", "Next tab"}, {style.Fold(), "Change value"}, {style.Enter(), "Edit / cycle"},
			{"d", "Delete rule"}, {"esc/q", "Save / discard"},
		}...)
	} else {
		keys = append(keys, [][2]string{
			{style.Fold(), "Fold"}, {style.Enter(), "Toggle"}, {"m", "Proxy / TUN"},
			{"a/d", "Add / Delete"}, {"t/T", "Ping"}, {"r/R", "Refresh"},
			{"o", "Settings"}, {"/", "Filter"}, {"q", "Quit"},
		}...)
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
