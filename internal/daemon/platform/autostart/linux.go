//go:build linux

package autostart

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const unit = `[Unit]
Description=justray background daemon
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=%s
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
func Enabled() bool {
	path, err := unitPath()
	if err != nil {
		return false
	}
	if _, err := os.Lstat(path); err != nil {
		return false
	}
	return exec.Command("systemctl", "--user", "is-enabled", "justrayd.service").Run() == nil
}

func symlinked(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSymlink != 0
}

func Enable() error {
	path, err := unitPath()
	if err != nil {
		return err
	}
	if symlinked(path) {
		return fmt.Errorf("%s is managed elsewhere", path)
	}

	bin, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, fmt.Appendf(nil, unit, bin), 0o600); err != nil {
		return err
	}

	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	if err := exec.Command("systemctl", "--user", "enable", "justrayd.service").Run(); err != nil {
		return errors.New("could not enable autostart")
	}
	return nil
}

func Disable() error {
	path, err := unitPath()
	if err != nil {
		return err
	}
	if symlinked(path) {
		return fmt.Errorf("%s is managed elsewhere", path)
	}

	_ = exec.Command("systemctl", "--user", "disable", "justrayd.service").Run()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	return nil
}
