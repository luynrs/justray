//go:build windows

package autostart

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"

	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
	"golang.org/x/sys/windows"
)

const name = "justrayd"

func cmd(ctx context.Context, name string, args ...string) *exec.Cmd {
	command := exec.CommandContext(ctx, name, args...)
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	return command
}

func Enabled(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	type result struct {
		enabled bool
		err     error
	}
	done := make(chan result, 1)
	go func() {
		enabled, err := func() (bool, error) {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			if err := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED); err != nil {
				if comError, ok := errors.AsType[*ole.OleError](err); !ok || comError.Code() != uintptr(windows.S_FALSE) {
					return false, err
				}
			}
			defer ole.CoUninitialize()
			object, err := oleutil.CreateObject("Schedule.Service")
			if err != nil {
				return false, err
			}
			defer object.Release()
			scheduler, err := object.QueryInterface(ole.IID_IDispatch)
			if err != nil {
				return false, err
			}
			defer scheduler.Release()
			connected, err := oleutil.CallMethod(scheduler, "Connect")
			if err != nil {
				return false, err
			}
			defer func() { _ = connected.Clear() }()
			folder, err := oleutil.CallMethod(scheduler, "GetFolder", `\`)
			if err != nil {
				return false, err
			}
			defer func() { _ = folder.Clear() }()
			task, err := oleutil.CallMethod(folder.ToIDispatch(), "GetTask", name)
			if err != nil {
				if comError, ok := errors.AsType[*ole.OleError](err); ok {
					if comError.Code() == 0x80070002 {
						return false, nil
					}
					if exception, ok := errors.AsType[ole.EXCEPINFO](comError.SubError()); ok && exception.SCODE() == 0x80070002 {
						return false, nil
					}
				}
				return false, err
			}
			defer func() { _ = task.Clear() }()
			value, err := oleutil.GetProperty(task.ToIDispatch(), "Enabled")
			if err != nil {
				return false, err
			}
			defer func() { _ = value.Clear() }()
			return value.Val != 0, nil
		}()
		done <- result{enabled, err}
	}()
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case completed := <-done:
		if err := ctx.Err(); err != nil {
			return false, err
		}
		return completed.enabled, completed.err
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
	_, queryErr := run(ctx, cmd(ctx, "schtasks", "/Query", "/TN", name, "/HRESULT"))
	if exit, ok := errors.AsType[*exec.ExitError](queryErr); ok && uint32(exit.ExitCode()) == 0x80070002 {
		return nil // already absent
	}
	return fmt.Errorf("disable autostart: %w", err)
}
