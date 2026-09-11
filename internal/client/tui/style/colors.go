package style

import (
	"charm.land/bubbles/v2/progress"
	"charm.land/lipgloss/v2"
)

var (
	green  = lipgloss.Color("2")
	yellow = lipgloss.Color("3")
	red    = lipgloss.Color("1")
	gray   = lipgloss.Color("8")
)

var (
	Title  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("4"))
	Accent = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	Strong = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	Name   = lipgloss.NewStyle().Bold(true)
	Dim    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	Err    = lipgloss.NewStyle().Bold(true).Foreground(red)

	pill    = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("4")).Bold(true)
	pillCap = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))

	Alive   = lipgloss.NewStyle().Foreground(green)
	Dead    = lipgloss.NewStyle().Foreground(red)
	Pending = lipgloss.NewStyle().Foreground(yellow)
	Unknown = lipgloss.NewStyle().Foreground(gray)
)

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
	case fraction >= 0.7:
		fill = yellow
	}
	b := progress.New(progress.WithColors(fill), progress.WithoutPercentage(), progress.WithWidth(12))
	b.EmptyColor = lipgloss.Color("8")
	return b.ViewAs(fraction)
}
