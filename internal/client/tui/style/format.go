package style

import (
	"fmt"
	"math"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/luynrs/justray/internal/shared/domain"
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
		t := lipgloss.NewStyle().MaxWidth(w-1).Render(s) + "…"
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

func Indent(s string) string {
	var b strings.Builder
	for line := range strings.Lines(s) {
		b.WriteString("  " + line)
	}
	return b.String()
}

func FirstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

func Sanitize(s string, emoji bool) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
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

func Since(t time.Time) string { return span(time.Since(t)) + " ago" }

func Uptime(d time.Duration) string {
	d = d.Round(time.Second)
	h, m, s := d/time.Hour, d/time.Minute%60, d/time.Second%60

	switch {
	case h > 0:
		return fmt.Sprintf("%dh %dm %ds", h, m, s)
	case m > 0:
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
	return span(d) + " left"
}

func span(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Hour-30*time.Second:
		return fmt.Sprintf("%dm", (d+30*time.Second)/time.Minute)
	case d < 24*time.Hour-30*time.Minute:
		return fmt.Sprintf("%dh", (d+30*time.Minute)/time.Hour)
	}
	return fmt.Sprintf("%dd", (d+12*time.Hour)/(24*time.Hour))
}

func Usage(t domain.Traffic) string {
	used := t.UploadBytes + t.DownloadBytes
	var parts []string
	switch {
	case t.TotalBytes > 0:
		parts = append(parts, fmt.Sprintf("%s %s %s",
			Bytes(used),
			Bar(float64(used)/float64(t.TotalBytes)),
			Bytes(t.TotalBytes)))
	case used > 0:
		parts = append(parts, Bytes(used)+" used")
	default:
		parts = append(parts, "No data")
	}
	if !t.ExpiresAt.IsZero() {
		parts = append(parts, Expiry(t.ExpiresAt))
	}
	return strings.Join(parts, " · ")
}
