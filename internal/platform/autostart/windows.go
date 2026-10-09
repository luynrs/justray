//go:build windows

package autostart

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

const name = "justrayd"

func cmd(ctx context.Context, name string, args ...string) *exec.Cmd {
	command := exec.CommandContext(ctx, name, args...)
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	return command
}

func Enabled(ctx context.Context) (bool, error) {
	output, err := run(ctx, cmd(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command",
		"$ErrorActionPreference = 'Stop'; if ((Get-ScheduledTask | Where-Object { $_.TaskName -eq '"+name+"' -and $_.TaskPath -eq '\\' }).Settings.Enabled) { 'on' } else { 'off' }"))
	if err != nil {
		return false, err
	}
	switch strings.TrimSpace(string(output)) {
	case "on":
		return true, nil
	case "off":
		return false, nil
	default:
		return false, errors.New("unrecognized scheduled task state")
	}
}

func Enable(ctx context.Context) error {
	bin, err := os.Executable()
	if err != nil {
		return err
	}
	ps := fmt.Sprintf(
		`$ErrorActionPreference = 'Stop'; Register-ScheduledTask -TaskName '%s' `+
			`-Action (New-ScheduledTaskAction -Execute '%s') `+
			`-Trigger (New-ScheduledTaskTrigger -AtLogOn) `+
			`-Principal (New-ScheduledTaskPrincipal -UserId $env:USERNAME -LogonType Interactive -RunLevel Highest) `+
			`-Settings (New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -ExecutionTimeLimit 0) `+
			`-Force`,
		name, strings.ReplaceAll(bin, "'", "''"),
	)
	_, err = run(ctx, cmd(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", ps))
	return err
}

func Disable(ctx context.Context) error {
	_, err := run(ctx, cmd(ctx, "schtasks", "/Delete", "/F", "/TN", name))
	if err == nil {
		return nil
	}
	var exit *exec.ExitError
	if _, queryErr := run(ctx, cmd(ctx, "schtasks", "/Query", "/TN", name, "/HRESULT")); errors.As(queryErr, &exit) && uint32(exit.ExitCode()) == 0x80070002 {
		return nil // already absent
	}
	return fmt.Errorf("disable autostart: %w", err)
}
