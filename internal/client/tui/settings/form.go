package settings

import (
	"cmp"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/luynrs/justray/internal/client/tui/style"
)

type fieldLayout struct {
	start int
	end   int
}

func (s *Model) View() string {
	extra := s.extra()
	lines := make([]string, min(s.height, s.total(extra)-s.scroll))
	first := sort.Search(len(s.rows), func(i int) bool { return s.bounds(i, extra).end > s.scroll })
	for i := first; i < len(s.rows); i++ {
		bounds := s.bounds(i, extra)
		if bounds.start >= s.scroll+s.height {
			break
		}
		for j, line := range s.block(s.rows[i], i == s.cursor) {
			position := bounds.start + j - s.scroll
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
	extra := s.extra()
	s.scroll = min(max(s.scroll, 0), max(s.total(extra)-s.height, 0))
	bounds := s.bounds(s.cursor, extra)
	if s.selecting() {
		position := bounds.start + 1 + s.option
		s.scroll = min(max(s.scroll, position-s.height+1), position)
	} else if s.input.Focused() {
		s.scroll = min(max(s.scroll, bounds.end-s.height), bounds.end-1)
	}
}

func (s *Model) extra() int {
	return max(s.at().options(), 1) - 1
}

func (s *Model) bounds(index, extra int) fieldLayout {
	bounds := s.layout[index]
	if index > s.cursor {
		bounds.start += extra
	}
	if index >= s.cursor {
		bounds.end += extra
	}
	return bounds
}

func (s *Model) total(extra int) int { return s.layout[len(s.layout)-1].end + extra }

func (s *Model) reveal() {
	bounds := s.bounds(s.cursor, s.extra())
	end := min(bounds.end, bounds.start+s.height)
	s.scroll = min(max(s.scroll, end-s.height), bounds.start)
	s.clamp()
}

func (s *Model) scrollBy(delta int) {
	s.scroll = min(max(s.scroll+delta, 0), max(s.total(s.extra())-s.height, 0))
}

func (s *Model) shownTabs() (int, []tab) {
	width := 0
	for _, tab := range tabs {
		width += style.SegmentWidth(" " + tab.name + " ")
	}
	if width > s.width-lipgloss.Width(style.Title())-2 {
		return s.tab, tabs[s.tab : s.tab+1]
	}
	return 0, tabs
}

func (s *Model) TabBar() string {
	first, visible := s.shownTabs()
	var b strings.Builder
	for i, t := range visible {
		b.WriteString(style.Segment(" "+t.name+" ", first+i == s.tab))
	}
	return b.String()
}

func (s *Model) tabAt(x int) (int, bool) {
	pos := style.RightStart(s.width, lipgloss.Width(style.Title()), lipgloss.Width(s.TabBar()))
	first, visible := s.shownTabs()
	for i, t := range visible {
		w := style.SegmentWidth(" " + t.name + " ")
		if x >= pos && x < pos+w {
			return first + i, true
		}
		pos += w
	}
	return 0, false
}

func (s *Model) block(f field, selected bool) []string {
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
		return []string{line}
	case f.list != nil:
		prefix, body := "+ ", f.name
		if f.removable() {
			prefix = style.Sep() + " "
			body = style.Sanitize(f.value(&s.cur), true)
		}

		if selected && s.input.Focused() {
			body = s.input.View()
		} else if !selected {
			body = style.Gray.Render(body)
		}
		return []string{bar + style.Gray.Render(prefix) + body}
	}
	return append([]string{bar + f.name}, s.value(f, selected, bar)...)
}

func (s *Model) value(f field, selected bool, bar string) (lines []string) {
	if selected && s.input.Focused() {
		return []string{bar + s.input.View()}
	}
	if options := f.options(); selected && options > 0 {
		for index := range options {
			var name string
			var checked bool
			if len(f.multi) > 0 {
				name = f.multi[index].name
				checked = f.multi[index].value(&s.cur) == "on"
			} else {
				name = f.enum[index]
				checked = name == f.value(&s.cur)
				name = label(name)
			}
			markerStyle := style.Gray
			if checked {
				markerStyle = style.Cyan
			}
			if index == s.option {
				name = style.Cyan.Render(name)
			} else {
				name = style.Gray.Render(name)
			}
			lines = append(lines, bar+markerStyle.Render(style.Dot(checked))+" "+name)
		}
		return lines
	}

	value := cmp.Or(f.value(&s.cur), f.hint, "Auto")
	if len(f.enum) > 0 {
		value = label(value)
	}
	return []string{bar + style.Gray.Render(value)}
}

func label(value string) string {
	switch value {
	case "ipv4":
		return "IPv4"
	case "ipv6":
		return "IPv6"
	case "gvisor":
		return "gVisor"
	}
	return strings.ToUpper(value[:1]) + value[1:]
}
