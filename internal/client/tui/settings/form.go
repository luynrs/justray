package settings

import (
	"cmp"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/luynrs/justray/internal/client/tui/style"
)

type fieldLayout struct {
	start int
	end   int
}

func (s Model) View() string {
	rows := s.rows()
	layout, total := s.layout(rows)
	lines := make([]string, min(s.height, total-s.scroll))
	for i, bounds := range layout {
		if bounds.end <= s.scroll {
			continue
		}
		if bounds.start >= s.scroll+s.height {
			break
		}
		offset := bounds.start
		if i > 0 && !rows[i].bare {
			offset--
		}
		for j, line := range s.fieldBlock(rows[i], i) {
			position := offset + j - s.scroll
			if position < 0 || position >= len(lines) {
				continue
			}
			lines[position] = style.Clip(line, s.width)
		}
	}
	return strings.Join(lines, "\n")
}

func (s *Model) Resize(width, height int) {
	s.width, s.height = width, max(height, 1)
	style.ResizeInput(&s.input, width-8)
	s.clamp()
}

func (s *Model) clamp() {
	layout, total := s.layout(s.rows())
	s.scroll = min(max(s.scroll, 0), max(total-s.height, 0))
	if s.input.Focused() {
		s.scroll = min(max(s.scroll, layout[s.cursor].end-s.height), layout[s.cursor].end-1)
	}
}

func (s Model) layout(rows []field) ([]fieldLayout, int) {
	layout := make([]fieldLayout, len(rows))
	position := 0
	for i, row := range rows {
		if i > 0 && !row.bare {
			position++
		}
		layout[i].start = position
		position++
		if row.editable() && !row.bare {
			if i == s.cursor && !s.input.Focused() {
				position += max(len(row.enum), 1)
			} else {
				position++
			}
		}
		layout[i].end = position
	}
	return layout, position
}

func (s *Model) reveal(layout []fieldLayout, total int) {
	s.scroll = min(max(s.scroll, 0), max(total-s.height, 0))
	start, end := layout[s.cursor].start, layout[s.cursor].end
	end = min(end, start+s.height)
	s.scroll = min(max(s.scroll, end-s.height), start)
}

func (s *Model) scrollBy(delta int) {
	_, total := s.layout(s.rows())
	s.scroll = min(max(s.scroll+delta, 0), max(total-s.height, 0))
}

func (s Model) compactTabs() bool {
	width := 0
	for _, tab := range tabs {
		width += lipgloss.Width(style.Segment(" "+tab.name+" ", false))
	}
	return width > s.width-lipgloss.Width(style.Title())-2
}

func (s Model) TabBar() string {
	if s.compactTabs() {
		return style.Segment(" "+tabs[s.tab].name+" ", true)
	}
	var b strings.Builder
	for i, t := range tabs {
		b.WriteString(style.Segment(" "+t.name+" ", i == s.tab))
	}
	return b.String()
}

func (s Model) tabAt(x int) (int, bool) {
	pos := max(s.width-lipgloss.Width(s.TabBar()), lipgloss.Width(style.Title())+2)
	if s.compactTabs() {
		width := lipgloss.Width(style.Segment(" "+tabs[s.tab].name+" ", true))
		return s.tab, x >= pos && x < pos+width
	}
	for i, t := range tabs {
		w := lipgloss.Width(style.Segment(" "+t.name+" ", false))
		if x >= pos && x < pos+w {
			return i, true
		}
		pos += w
	}
	return 0, false
}

// fieldBlock renders one row, blank line above non-list rows
func (s Model) fieldBlock(f field, i int) (lines []string) {
	selected := i == s.cursor

	bar := "  "
	if selected {
		bar = style.Cyan.Render(style.Bar())
	}

	switch {
	case !f.editable():
		line := bar + f.name
		if f.hint != "" {
			line += " " + style.Gray.Render(f.hint)
		}
		lines = []string{line}
	case f.bare:
		var prefix, body string
		if f.removable() {
			prefix = style.Sep() + " "
			body = f.name
		} else {
			prefix = "+ "
			body = strings.TrimPrefix(f.name, "+ ")
		}

		if selected && s.input.Focused() {
			body = s.input.View()
		} else if !selected {
			body = style.Gray.Render(body)
		}
		text := style.Gray.Render(prefix) + body
		lines = []string{bar + text}
	default:
		lines = []string{bar + f.name}
		lines = append(lines, s.valueLines(f, selected, bar)...)
	}

	if i > 0 && !f.bare {
		lines = append([]string{""}, lines...)
	}
	return lines
}

func (s Model) valueLines(f field, selected bool, bar string) (lines []string) {
	if selected && s.input.Focused() {
		return []string{bar + s.input.View()}
	}

	if len(f.enum) > 0 && selected {
		cur := f.value(&s.cur)
		for _, opt := range f.enum {
			line := bar + style.Gray.Render(style.Dot(false)+" "+opt)
			if opt == cur {
				line = bar + style.Cyan.Render(style.Dot(true)+" "+opt)
			}
			lines = append(lines, line)
		}
		return lines
	}

	v := f.value(&s.cur)
	if v == "" {
		v = cmp.Or(f.hint, "auto")
	}
	return []string{bar + style.Gray.Render(v)}
}
