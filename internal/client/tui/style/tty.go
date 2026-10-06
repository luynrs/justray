package style

import (
	"cmp"
	"os"
	"strings"

	"charm.land/bubbles/v2/spinner"
)

var TTY bool

func DetectTTY(force string) bool {
	if force == "on" {
		return true
	}
	term := strings.ToLower(os.Getenv("TERM"))
	if term == "linux" || term == "dumb" || strings.HasPrefix(term, "vt") || strings.HasPrefix(term, "cons") {
		return true
	}
	loc := strings.ToUpper(cmp.Or(os.Getenv("LC_ALL"), os.Getenv("LC_CTYPE"), os.Getenv("LANG")))
	return loc == "C" || loc == "POSIX"
}

func pick(tty, rich string) string {
	if TTY {
		return tty
	}
	return rich
}

func Bar() string   { return pick("| ", "▎ ") }
func Sep() string   { return pick("-", "·") }
func Move() string  { return pick("j/k", "↑/↓") }
func Fold() string  { return pick("h/l", "←/→") }
func Enter() string { return pick("enter", "↵") }

func Spinner() spinner.Spinner {
	if TTY {
		return spinner.Line
	}
	return spinner.MiniDot
}

func Arrow(collapsed bool) string {
	if collapsed {
		return pick(">", "▶")
	}
	return pick("v", "▼")
}

func Dot(filled bool) string {
	if filled {
		return pick("*", "●")
	}
	return pick("o", "○")
}

func Check() string { return pick("+", "✓") }
func Cross() string { return pick("x", "✗") }

func Branch(last bool) string {
	if TTY {
		return ""
	}
	if last {
		return "└─"
	}
	return "├─"
}
