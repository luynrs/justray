package tui

import (
	"github.com/luynrs/justray/internal/client/tui/navigation"
	"github.com/luynrs/justray/internal/client/tui/tree"
)

func (m *Model) navigate(motion navigation.Motion) {
	if motion.Action == 0 {
		return
	}
	rows := m.rows()
	selectable := tree.Selectable(rows)
	if len(selectable) == 0 {
		m.cursor, m.scroll = 0, 0
		return
	}
	height := m.height()
	switch motion.Action {
	case navigation.Move:
		m.cursor += motion.Distance(height, len(selectable))
	case navigation.Page, navigation.HalfPage:
		m.cursor, m.scroll = tree.Page(rows, m.cursor, m.scroll, motion.Distance(height, len(rows)), height)
		return
	case navigation.Scroll:
		m.cursor, m.scroll = tree.Clamp(rows, m.cursor, m.scroll+motion.Distance(height, len(rows)), height)
		return
	case navigation.Line:
		m.cursor = min(motion.Count-1, len(selectable)-1)
	case navigation.Last:
		m.cursor = len(selectable) - 1
	}
	m.cursor, m.scroll = tree.Reveal(rows, m.cursor, m.scroll, height)
}
