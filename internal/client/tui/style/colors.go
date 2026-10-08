package style

import (
	"strings"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
)

var (
	green  = lipgloss.Color("2")
	yellow = lipgloss.Color("3")
	red    = lipgloss.Color("1")
)

var (
	Title      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("4"))
	MutedTitle = Title.Foreground(lipgloss.Color("#3d4a52"))
	Accent     = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	Strong     = Accent.Bold(true)
	Name       = lipgloss.NewStyle().Bold(true)
	Key        = Name
	Dim        = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	Err        = lipgloss.NewStyle().Bold(true).Foreground(red)

	pill    = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(Title.GetForeground()).Bold(true)
	pillCap = lipgloss.NewStyle().Foreground(Title.GetForeground())

	Alive   = lipgloss.NewStyle().Foreground(green)
	Dead    = lipgloss.NewStyle().Foreground(red)
	Pending = lipgloss.NewStyle().Foreground(yellow)

	Input = textinput.Styles{
		Focused: textinput.StyleState{Placeholder: Dim, Suggestion: Dim},
		Blurred: textinput.StyleState{Text: Dim, Placeholder: Dim, Suggestion: Dim},
		Cursor:  textinput.CursorStyle{Color: Accent.GetForeground(), Blink: true},
	}
	Editor = textarea.Styles{
		Focused: textarea.StyleState{Placeholder: Dim, Selection: lipgloss.NewStyle().Reverse(true)},
		Blurred: textarea.StyleState{Text: Dim, Placeholder: Dim, Selection: lipgloss.NewStyle().Reverse(true)},
		Cursor:  textarea.CursorStyle{Color: Accent.GetForeground(), Blink: true},
	}
)

func KeyHint(key, description string) string {
	return Key.Render(key) + Dim.Render(" "+description)
}

// Segment is one tab, same width either way
func Segment(s string, active bool) string {
	if !active {
		return " " + Dim.Render(s) + " "
	}
	if TTY {
		return Strong.Render("[" + s + "]")
	}
	return pillCap.Render("▐") + pill.Render(s) + pillCap.Render("▌")
}

func Progress(fraction float64) string {
	fill := green
	switch {
	case fraction >= 0.9:
		fill = red
	case fraction >= 0.75:
		fill = yellow
	}
	n := max(0, min(12, int(fraction*12+0.5)))
	full, empty := pick("=", "█"), pick("-", "░")
	return lipgloss.NewStyle().Foreground(fill).Render(strings.Repeat(full, n)) +
		Dim.Render(strings.Repeat(empty, 12-n))
}
