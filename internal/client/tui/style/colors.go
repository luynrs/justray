package style

import (
	"strings"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"

	"github.com/luynrs/justray/internal/version"
)

var (
	Red    = lipgloss.NewStyle().Foreground(lipgloss.Red)
	Green  = lipgloss.NewStyle().Foreground(lipgloss.Green)
	Yellow = lipgloss.NewStyle().Foreground(lipgloss.Yellow)
	Blue   = lipgloss.NewStyle().Foreground(lipgloss.Blue)
	Cyan   = lipgloss.NewStyle().Foreground(lipgloss.Cyan)
	White  = lipgloss.NewStyle().Foreground(lipgloss.White)

	Gray = lipgloss.NewStyle().Foreground(lipgloss.BrightBlack)

	Bold = lipgloss.NewStyle().Bold(true)

	Input = textinput.Styles{
		Focused: textinput.StyleState{Prompt: Gray, Placeholder: Gray, Suggestion: Gray},
		Blurred: textinput.StyleState{Prompt: Gray, Text: Gray, Placeholder: Gray, Suggestion: Gray},
		Cursor:  textinput.CursorStyle{Color: Cyan.GetForeground(), Blink: true},
	}
	Editor = textarea.Styles{
		Focused: textarea.StyleState{Placeholder: Gray, Selection: lipgloss.NewStyle().Reverse(true)},
		Blurred: textarea.StyleState{Text: Gray, Placeholder: Gray, Selection: lipgloss.NewStyle().Reverse(true)},
		Cursor:  textarea.CursorStyle{Color: Cyan.GetForeground(), Blink: true},
	}
)

func Title() string {
	return Blue.Bold(true).Render("JustRay") + " " + Gray.Render(version.String())
}

func ResizeInput(input *textinput.Model, width int) {
	width = max(width, 1)
	if input.Width() == width {
		return
	}
	position := input.Position()
	input.SetWidth(width)
	input.CursorEnd()
	input.SetCursor(position)
}

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
	return White.Background(Blue.GetForeground()).Bold(true).Render(" " + s + " ")
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
