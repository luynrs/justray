package style

import (
	"strings"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
)

var (
	Black   = lipgloss.NewStyle().Foreground(lipgloss.Black)
	Red     = lipgloss.NewStyle().Foreground(lipgloss.Red)
	Green   = lipgloss.NewStyle().Foreground(lipgloss.Green)
	Yellow  = lipgloss.NewStyle().Foreground(lipgloss.Yellow)
	Blue    = lipgloss.NewStyle().Foreground(lipgloss.Blue)
	Magenta = lipgloss.NewStyle().Foreground(lipgloss.Magenta)
	Cyan    = lipgloss.NewStyle().Foreground(lipgloss.Cyan)
	White   = lipgloss.NewStyle().Foreground(lipgloss.White)

	Gray          = lipgloss.NewStyle().Foreground(lipgloss.BrightBlack)
	BrightRed     = lipgloss.NewStyle().Foreground(lipgloss.BrightRed)
	BrightGreen   = lipgloss.NewStyle().Foreground(lipgloss.BrightGreen)
	BrightYellow  = lipgloss.NewStyle().Foreground(lipgloss.BrightYellow)
	BrightBlue    = lipgloss.NewStyle().Foreground(lipgloss.BrightBlue)
	BrightMagenta = lipgloss.NewStyle().Foreground(lipgloss.BrightMagenta)
	BrightCyan    = lipgloss.NewStyle().Foreground(lipgloss.BrightCyan)
	BrightWhite   = lipgloss.NewStyle().Foreground(lipgloss.BrightWhite)

	Bold = lipgloss.NewStyle().Bold(true)

	Input = textinput.Styles{
		Focused: textinput.StyleState{Placeholder: Gray, Suggestion: Gray},
		Blurred: textinput.StyleState{Text: Gray, Placeholder: Gray, Suggestion: Gray},
		Cursor:  textinput.CursorStyle{Color: Cyan.GetForeground(), Blink: true},
	}
	Editor = textarea.Styles{
		Focused: textarea.StyleState{Placeholder: Gray, Selection: lipgloss.NewStyle().Reverse(true)},
		Blurred: textarea.StyleState{Text: Gray, Placeholder: Gray, Selection: lipgloss.NewStyle().Reverse(true)},
		Cursor:  textarea.CursorStyle{Color: Cyan.GetForeground(), Blink: true},
	}
)

func KeyHint(key, description string) string {
	return Bold.Render(key) + Gray.Render(" "+description)
}

func Segment(s string, active bool) string {
	if !active {
		return " " + Gray.Render(s) + " "
	}
	if TTY {
		return Cyan.Bold(true).Render("[" + s + "]")
	}
	return Blue.Render("▐") + Blue.Bold(true).Reverse(true).Render(s) + Blue.Render("▌")
}

func Progress(fraction float64) string {
	fill := Green
	switch {
	case fraction >= 0.9:
		fill = Red
	case fraction >= 0.75:
		fill = Yellow
	}
	n := max(0, min(12, int(fraction*12+0.5)))
	full, empty := pick("=", "█"), pick("-", "░")
	return fill.Render(strings.Repeat(full, n)) + Gray.Render(strings.Repeat(empty, 12-n))
}
