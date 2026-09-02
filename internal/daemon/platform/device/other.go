//go:build !windows

package device

import (
	"cmp"
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

func Info(ctx context.Context) (osName, hwid, ver, model string) {
	switch runtime.GOOS {
	case "linux":
		return "Linux", cmp.Or(read("/etc/machine-id"), read("/var/lib/dbus/machine-id")), distro(), read("/sys/devices/virtual/dmi/id/product_name")
	case "darwin":
		_, hwid, _ := strings.Cut(run(ctx, "ioreg", "-rd1", "-c", "IOPlatformExpertDevice"), `"IOPlatformUUID" = "`)
		hwid, _, _ = strings.Cut(hwid, `"`)
		return "macOS", hwid, run(ctx, "sw_vers", "-productVersion"), run(ctx, "sysctl", "-n", "hw.model")
	default:
		return runtime.GOOS, "", "", ""
	}
}

func distro() string {
	for line := range strings.Lines(read("/etc/os-release")) {
		if name, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
			return strings.Trim(strings.TrimSpace(name), `"`)
		}
	}
	return ""
}

func read(path string) string {
	data, _ := os.ReadFile(path)
	return strings.TrimSpace(string(data))
}

func run(ctx context.Context, name string, args ...string) string {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	out, _ := cmd.Output()
	return strings.TrimSpace(string(out))
}
