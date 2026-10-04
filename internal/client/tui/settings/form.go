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

func (s *Model) View(width, height int) string {
	w := max(width-2, 20)
	s.input.SetWidth(max(w-6, 12))

	rows := s.rows()
	heights := make([]int, len(rows))
	for i, f := range rows {
		heights[i] = 1
		if f.editable() && !f.bare {
			if i == s.cursor && !s.input.Focused() {
				heights[i] += max(len(f.enum), 1)
			} else {
				heights[i]++
			}
		}
		if i > 0 && !f.bare {
			heights[i]++
		}
	}

	h := max(height, 1)
	s.scrollTo(heights, h)

	lines := make([]string, 0, h)
	hits := map[int]hit{}
	for i := s.scroll; i < len(rows); i++ {
		if len(lines)+heights[i] > h && i > s.scroll {
			break
		}
		block, choices := s.fieldBlock(rows[i], i)
		for j, line := range block {
			if len(lines) >= h {
				break
			}
			line = style.Clip(line, width)
			hits[len(lines)] = hit{row: i, choice: choices[j]}
			lines = append(lines, line)
		}
	}
	s.hits = hits
	return strings.Join(lines, "\n")
}

func (s *Model) scrollTo(heights []int, h int) {
	s.scroll = min(max(s.scroll, 0), max(len(heights)-1, 0))
	if s.cursor < s.scroll {
		s.scroll = s.cursor
	}
	visibleHeight := 0
	for _, height := range heights[s.scroll : s.cursor+1] {
		visibleHeight += height
	}
	for s.scroll < s.cursor && visibleHeight > h {
		visibleHeight -= heights[s.scroll]
		s.scroll++
	}
	for s.scroll > 0 && visibleHeight+heights[s.scroll-1] <= h {
		s.scroll--
		visibleHeight += heights[s.scroll]
	}
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
