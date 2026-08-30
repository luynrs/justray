package subscription

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/luynrs/justray/internal/daemon/platform/console"
	"github.com/luynrs/justray/internal/daemon/platform/device"
	"github.com/luynrs/justray/internal/shared/version"
)

// Device info; X-Hwid is required, the rest cosmetic
func deviceHeaders(ctx context.Context) (http.Header, error) {
	run := func(name string, arg ...string) string { return runCommand(ctx, name, arg...) }
	h := http.Header{}
	set := func(key, val string) {
		if val != "" {
			h.Set(key, val)
		}
	}
	set("User-Agent", "justray/"+version.String())

	switch runtime.GOOS {
	case "linux":
		set("X-Device-OS", "Linux")
		set("X-Hwid", hash(cmp.Or(readFile("/etc/machine-id"), readFile("/var/lib/dbus/machine-id"))))
		set("X-Ver-OS", distro())
		set("X-Device-Model", readFile("/sys/devices/virtual/dmi/id/product_name"))
	case "darwin":
		_, uuid, _ := strings.Cut(run("ioreg", "-rd1", "-c", "IOPlatformExpertDevice"), `"IOPlatformUUID" = "`)
		uuid, _, _ = strings.Cut(uuid, `"`)
		set("X-Device-OS", "macOS")
		set("X-Hwid", hash(uuid))
		set("X-Ver-OS", run("sw_vers", "-productVersion"))
		set("X-Device-Model", run("sysctl", "-n", "hw.model"))
	case "windows":
		hwid, ver, model := device.Info()
		set("X-Device-OS", "Windows")
		set("X-Hwid", hash(hwid))
		set("X-Ver-OS", ver)
		set("X-Device-Model", model)
	default:
		set("X-Device-OS", runtime.GOOS)
	}

	if h.Get("X-Hwid") == "" {
		return h, fmt.Errorf("no machine id on %s", runtime.GOOS)
	}
	return h, nil
}

func hash(raw string) string {
	if raw == "" {
		return ""
	}
	sum := sha256.Sum256([]byte("justray:" + raw))
	return hex.EncodeToString(sum[:16]) // 32 hex chars to match [a-zA-Z0-9=-]{10,64}$
}

func distro() string {
	for line := range strings.SplitSeq(readFile("/etc/os-release"), "\n") {
		if name, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
			return strings.Trim(name, `"`)
		}
	}
	return ""
}

func readFile(p string) string {
	data, _ := os.ReadFile(p)
	return strings.TrimSpace(string(data))
}

func runCommand(ctx context.Context, name string, arg ...string) string {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, arg...)
	console.Hide(cmd)
	out, _ := cmd.Output()
	return strings.TrimSpace(string(out))
}
