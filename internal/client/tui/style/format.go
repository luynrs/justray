package style

import (
	"fmt"
	"math"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/luynrs/justray/internal/domain"
)

func Clip(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(s)
}

func Pad(s string, w int) string {
	switch n := lipgloss.Width(s); {
	case w <= 0:
		return ""
	case n > w:
		tail := pick("..", "…")
		tw := lipgloss.Width(tail)
		if w <= tw {
			return tail[:w]
		}
		t := lipgloss.NewStyle().MaxWidth(w-tw).Render(s) + tail
		if shortfall := w - lipgloss.Width(t); shortfall > 0 {
			t += strings.Repeat(" ", shortfall)
		}
		return t
	default:
		return s + strings.Repeat(" ", w-n)
	}
}

// Flush right-aligns right, at least two spaces apart
func Flush(left, right string, width int) string {
	if right == "" {
		return left
	}
	gap := max(width-lipgloss.Width(left)-lipgloss.Width(right), 2)
	return left + strings.Repeat(" ", gap) + right
}

func Fit(body string, n int) string {
	lines := strings.Split(body, "\n")
	for len(lines) < n {
		lines = append(lines, "")
	}
	return strings.Join(lines[:max(n, 0)], "\n")
}

func FirstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

func Sanitize(s string, emoji bool) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if (r < 0x20 && r != '\n' && r != '\t') || r == 0x7f {
			return -1
		}
		if !emoji {
			switch {
			case r >= 0x1f000 && r <= 0x1ffff,
				r >= 0x2600 && r <= 0x27bf,
				r >= 0x2b00 && r <= 0x2bff,
				r >= 0x2190 && r <= 0x21ff,
				r >= 0x2300 && r <= 0x23ff,
				r >= 0x25a0 && r <= 0x25ff,
				r == 0x203c, r == 0x2049, r == 0x2122, r == 0x2139,
				r == 0x24c2, r == 0x2934, r == 0x2935,
				r == 0x3030, r == 0x303d, r == 0x3297, r == 0x3299,
				r == 0xfe0f, r == 0x200d, r == 0x20e3:
				return -1
			}
		}
		return r
	}, s))
}

func Bytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

func Since(t time.Time) string {
	d := max(0, time.Since(t))
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

func Uptime(d time.Duration) string {
	d = max(0, d.Round(time.Second))
	days := int(d.Hours()) / 24
	h, m, s := int(d.Hours())%24, int(d.Minutes())%60, int(d.Seconds())%60

	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, h, m)
	}
	if h > 0 {
		return fmt.Sprintf("%dh %dm %ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func Expiry(t time.Time) string {
	d := time.Until(t)
	if d < 0 {
		return "expired " + Since(t)
	}
	if d > math.MaxInt64/2 {
		return t.Format("2006-01-02")
	}
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm left", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh left", int(d.Hours()))
	}
	return fmt.Sprintf("%dd left", int(d.Hours()/24))
}

func Usage(t domain.Traffic) string {
	used := t.UploadBytes + t.DownloadBytes
	var parts []string
	switch {
	case t.TotalBytes > 0:
		parts = append(parts, fmt.Sprintf("%s %s %s",
			Dim.Render(Bytes(used)),
			Progress(float64(used)/float64(t.TotalBytes)),
			Dim.Render(Bytes(t.TotalBytes))))
	case used > 0:
		parts = append(parts, Dim.Render(Bytes(used)+" used"))
	default:
		parts = append(parts, Dim.Render("No data"))
	}
	if !t.ExpiresAt.IsZero() {
		parts = append(parts, Dim.Render(Expiry(t.ExpiresAt)))
	}
	return strings.Join(parts, Dim.Render(" "+Sep()+" "))
}
