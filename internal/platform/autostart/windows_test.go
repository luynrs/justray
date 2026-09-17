//go:build windows

package autostart

import (
	"os"
	"testing"
)

func TestDisable(t *testing.T) {
	if os.Getenv("JUSTRAY_TEST_SCHEDULER") != "1" {
		t.Skip("set JUSTRAY_TEST_SCHEDULER=1 to run task tests")
	}
	for range 2 {
		if err := Disable(); err != nil {
			t.Fatal(err)
		}
	}
}
