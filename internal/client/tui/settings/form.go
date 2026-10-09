package settings

import (
	"cmp"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/luynrs/justray/internal/client/tui/style"
	"github.com/luynrs/justray/internal/version"
)

type hit struct {
	row    int
	choice string
}

type fieldLayout struct {
	start int
	end   int
}

func (s *Model) View(width, height int) string {
	w := max(width-2, 20)
	s.input.SetWidth(max(w-6, 12))
	s.height = max(height, 1)

	rows := s.rows()
	layout, total := s.layout(rows)
	s.scroll = min(max(s.scroll, 0), max(total-s.height, 0))
	lines := make([]string, min(s.height, total-s.scroll))
	hits := map[int]hit{}
	for i, bounds := range layout {
		if bounds.end <= s.scroll {
			continue
		}
		if bounds.start >= s.scroll+s.height {
			break
		}
		block, choices := s.fieldBlock(rows[i], i)
		offset := bounds.start
		if i > 0 && !rows[i].bare {
			offset--
		}
		for j, line := range block {
			position := offset + j - s.scroll
			if position < 0 || position >= len(lines) {
				continue
			}
			lines[position] = style.Clip(line, width)
			if line != "" {
				hits[position] = hit{row: i, choice: choices[j]}
			}
		}
	}
	s.hits = hits
	return strings.Join(lines, "\n")
}

func (s *Model) layout(rows []field) ([]fieldLayout, int) {
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

func (s *Model) TabBar(width int) string {
	var b strings.Builder
	for i, t := range tabs {
		b.WriteString(style.Segment(" "+t.name+" ", i == s.tab))
	}
	s.compactTabs = lipgloss.Width(b.String()) > width
	if !s.compactTabs {
		return b.String()
	}
	return style.Segment(" "+tabs[s.tab].name+" ", true) + style.Dim.Render(fmt.Sprintf(" %d/%d", s.tab+1, len(tabs)))
}

func (s *Model) tabAt(x int) (int, bool) {
	pos := lipgloss.Width(style.Title.Render("JustRay")+" "+style.Dim.Render(version.String())) + 2
	if s.compactTabs {
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
func (s *Model) fieldBlock(f field, i int) (lines, choices []string) {
	selected := i == s.cursor

	bar := "  "
	if selected {
		bar = style.Accent.Render(style.Bar())
	}

	switch {
	case !f.editable():
		line := bar + f.name
		if f.hint != "" {
			line += " " + style.Dim.Render(f.hint)
		}
		lines, choices = []string{line}, []string{""}
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
			body = style.Dim.Render(body)
		}
		text := style.Dim.Render(prefix) + body
		lines, choices = []string{bar + text}, []string{""}
	default:
		lines = []string{bar + f.name}
		choices = []string{""}
		value, picks := s.valueLines(f, selected, bar)
		lines = append(lines, value...)
		choices = append(choices, picks...)
	}

	if i > 0 && !f.bare {
		lines = append([]string{""}, lines...)
		choices = append([]string{""}, choices...)
	}
	return lines, choices
}

func (s *Model) valueLines(f field, selected bool, bar string) (lines, choices []string) {
	if selected && s.input.Focused() {
		return []string{bar + s.input.View()}, []string{""}
	}

	if len(f.enum) > 0 && selected {
		cur := f.value(s.cur)
		for _, opt := range f.enum {
			line := bar + style.Dim.Render(style.Dot(false)+" "+opt)
			if opt == cur {
				line = bar + style.Accent.Render(style.Dot(true)+" "+opt)
			}
			lines = append(lines, line)
			choices = append(choices, opt)
		}
		return lines, choices
	}

	v := f.value(s.cur)
	if v == "" {
		v = cmp.Or(f.hint, "auto")
	}
	return []string{bar + style.Dim.Render(v)}, []string{""}
}
