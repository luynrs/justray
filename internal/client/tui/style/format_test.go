package style

import (
	"testing"
	"time"

	"charm.land/lipgloss/v2"
)

func TestUptime(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{
		{-5 * time.Second, "0s"},
		{0, "0s"},
		{30 * time.Second, "30s"},
		{5*time.Minute + 12*time.Second, "5m 12s"},
		{2*time.Hour + 3*time.Minute + 4*time.Second, "2h 3m 4s"},
		{27*time.Hour + 3*time.Minute, "1d 3h 3m"},
		{50 * time.Hour, "2d 2h 0m"},
	} {
		if got := Uptime(tc.d); got != tc.want {
			t.Errorf("Uptime(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestPad(t *testing.T) {
	for _, w := range []int{0, 1, 2, 5, 10, 20} {
		got := Pad("very long text string", w)
		if n := lipgloss.Width(got); n > w {
			t.Errorf("Pad(w=%d) width = %d, want <= %d", w, n, w)
		}
	}
}
