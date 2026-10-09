//go:build darwin

package autostart

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/luynrs/justray/internal/ipc"
)

const label = "com.github.luynrs.justrayd"

const task = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>%s</string>
	<key>ProgramArguments</key><array><string>%s</string></array>
	<key>RunAtLoad</key><true/>
	<key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict>
</dict>
</plist>
`

func plistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", label+".plist"), nil
}

func Enabled(ctx context.Context) (bool, error) {
	path, err := plistPath()
	if err != nil {
		return false, err
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	output, err := run(ctx, exec.CommandContext(ctx, "launchctl", "print-disabled", guiDomain()))
	if err != nil {
		return false, err
	}
	for line := range strings.SplitSeq(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[0] == strconv.Quote(label) && fields[1] == "=>" {
			switch fields[2] {
			case "false", "enabled":
				return true, nil
			case "true", "disabled":
				return false, nil
			default:
				return false, errors.New("unrecognized launchctl state: " + fields[2])
			}
		}
	}
	return true, nil
}

func guiDomain() string {
	if os.Geteuid() == 0 {
		return "gui/" + os.Getenv("JUSTRAY_UID")
	}
	return "gui/" + strconv.Itoa(os.Getuid())
}

func Enable(ctx context.Context) error {
	path, err := plistPath()
	if err != nil {
		return err
	}
	bin, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := ipc.Chown(filepath.Dir(path)); err != nil {
		return err
	}
	var command bytes.Buffer
	_ = xml.EscapeText(&command, []byte(bin))
	if err := os.WriteFile(path, fmt.Appendf(nil, task, label, command.String()), 0o600); err != nil {
		return err
	}
	if err := ipc.Chown(path); err != nil {
		return err
	}
	if _, err := run(ctx, exec.CommandContext(ctx, "launchctl", "enable", guiDomain()+"/"+label)); err != nil {
		return err
	}
	if _, err := run(ctx, exec.CommandContext(ctx, "launchctl", "print", guiDomain()+"/"+label)); err != nil {
		if _, err := run(ctx, exec.CommandContext(ctx, "launchctl", "bootstrap", guiDomain(), path)); err != nil {
			return err
		}
	}
	return nil
}

func Disable(ctx context.Context) error {
	path, err := plistPath()
	if err != nil {
		return err
	}
	if _, err := run(ctx, exec.CommandContext(ctx, "launchctl", "disable", guiDomain()+"/"+label)); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
