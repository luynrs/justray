//go:build windows

package autostart

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

const name = "justrayd"

func cmd(args ...string) *exec.Cmd {
	c := exec.Command("schtasks", args...)
	c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	return c
}

func Enabled() bool {
	return cmd("/Query", "/TN", name).Run() == nil
}

func Enable() error {
	bin, err := os.Executable()
	if err != nil {
		return err
	}
	ps := fmt.Sprintf(
		`Register-ScheduledTask -TaskName '%s' `+
			`-Action (New-ScheduledTaskAction -Execute '%s') `+
			`-Trigger (New-ScheduledTaskTrigger -AtLogOn) `+
			`-Principal (New-ScheduledTaskPrincipal -UserId $env:USERNAME -LogonType Interactive -RunLevel Highest) `+
			`-Settings (New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -ExecutionTimeLimit 0) `+
			`-Force`,
		name, strings.ReplaceAll(bin, "'", "''"),
	)
	c := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", ps)
	c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	if out, err := c.CombinedOutput(); err != nil {
		if msg := string(bytes.TrimSpace(out)); msg != "" {
			return fmt.Errorf("enable autostart: %s", msg)
		}
		return fmt.Errorf("enable autostart: %w", err)
	}
	return nil
}

func Disable() error {
	out, err := cmd("/Delete", "/F", "/TN", name).CombinedOutput()
	if err == nil {
		return nil
	}
	var exit *exec.ExitError
	if queryErr := cmd("/Query", "/TN", name, "/HRESULT").Run(); errors.As(queryErr, &exit) && uint32(exit.ExitCode()) == 0x80070002 {
		return nil // already absent
	}
	if msg := string(bytes.TrimSpace(out)); msg != "" {
		return fmt.Errorf("disable autostart: %s", msg)
	}
	return fmt.Errorf("disable autostart: %w", err)
}
