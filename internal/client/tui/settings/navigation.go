package settings

import "github.com/luynrs/justray/internal/client/tui/navigation"

func (s *Model) navigate(motion navigation.Motion) {
	if motion.Action == 0 {
		return
	}
	rows := s.rows()
	switch motion.Action {
	case navigation.Move:
		s.move(motion.Distance(s.height, len(rows)))
		return
	case navigation.Line, navigation.Last:
		s.cursor = 0
		if motion.Action == navigation.Last {
			s.move(len(rows))
		} else {
			s.move(motion.Count - 1)
		}
		return
	}
	layout, total := s.layout(rows)
	switch motion.Action {
	case navigation.Page, navigation.HalfPage:
		delta := motion.Distance(s.height, total)
		target := layout[s.cursor].start + delta
		if delta > 0 {
			for s.cursor < len(rows)-1 && (layout[s.cursor].start < target || !rows[s.cursor].editable()) {
				s.cursor++
			}
		} else {
			for s.cursor > 0 && (layout[s.cursor].start > target || !rows[s.cursor].editable()) {
				s.cursor--
			}
		}
		s.scroll += delta
		layout, total = s.layout(rows)
		s.reveal(layout, total)
	case navigation.Scroll:
		s.scrollBy(motion.Distance(s.height, total))
	}
}
