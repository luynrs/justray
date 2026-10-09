//go:build linux

package autostart

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const unit = `[Unit]
Description=justray background daemon
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=:%q
Restart=on-failure
RestartSec=3

[Install]
WantedBy=default.target
`

func unitPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "systemd", "user", "justrayd.service"), nil
}

// Enabled counts foreign units as enabled too
func Enabled(ctx context.Context) (bool, error) {
	path, err := unitPath()
	if err != nil {
		return false, err
	}
	if _, err := os.Lstat(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	output, err := run(ctx, exec.CommandContext(ctx, "systemctl", "--user", "is-enabled", "justrayd.service"))
	if err == nil {
		return true, nil
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return false, err
	}
	switch strings.TrimSpace(string(output)) {
	case "disabled", "masked", "masked-runtime", "linked", "linked-runtime", "not-found":
		return false, nil
	}
	return false, err
}

func symlinked(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSymlink != 0
}

func Enable(ctx context.Context) error {
	path, err := unitPath()
	if err != nil {
		return err
	}
	if symlinked(path) {
		return fmt.Errorf("%s is managed elsewhere", path)
	}

	bin, err := exec.LookPath(os.Args[0])
	if err != nil && !errors.Is(err, exec.ErrDot) {
		return err
	}
	bin, err = filepath.Abs(bin)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, fmt.Appendf(nil, unit, strings.ReplaceAll(bin, "%", "%%")), 0o600); err != nil {
		return err
	}

	if _, err := run(ctx, exec.CommandContext(ctx, "systemctl", "--user", "daemon-reload")); err != nil {
		return err
	}
	_, err = run(ctx, exec.CommandContext(ctx, "systemctl", "--user", "enable", "justrayd.service"))
	return err
}

func Disable(ctx context.Context) error {
	path, err := unitPath()
	if err != nil {
		return err
	}
	if symlinked(path) {
		return fmt.Errorf("%s is managed elsewhere", path)
	}

	if _, err := run(ctx, exec.CommandContext(ctx, "systemctl", "--user", "disable", "justrayd.service")); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	_, err = run(ctx, exec.CommandContext(ctx, "systemctl", "--user", "daemon-reload"))
	return err
}
