//go:build windows

package autostart

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"runtime"

	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
	"golang.org/x/sys/windows"
)

const name = "justrayd"

const task = `<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
	<Triggers><LogonTrigger><Enabled>true</Enabled></LogonTrigger></Triggers>
	<Principals><Principal id="Author">
		<LogonType>InteractiveToken</LogonType>
		<RunLevel>HighestAvailable</RunLevel>
	</Principal></Principals>
	<Settings>
		<DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
		<StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
		<ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
	</Settings>
	<Actions Context="Author"><Exec><Command>%s</Command></Exec></Actions>
</Task>`

func isNotFound(err error) bool {
	if comError, ok := errors.AsType[*ole.OleError](err); ok {
		if uint32(comError.Code()) == 0x80070002 {
			return true
		}
		if exception, ok := errors.AsType[ole.EXCEPINFO](comError.SubError()); ok && exception.SCODE() == 0x80070002 {
			return true
		}
	}
	return false
}

func withFolder(ctx context.Context, action func(folder *ole.IDispatch) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED); err != nil {
		if comError, ok := errors.AsType[*ole.OleError](err); !ok || comError.Code() != uintptr(windows.S_FALSE) {
			return err
		}
	}
	defer ole.CoUninitialize()
	object, err := oleutil.CreateObject("Schedule.Service")
	if err != nil {
		return err
	}
	defer object.Release()
	scheduler, err := object.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return err
	}
	defer scheduler.Release()
	connected, err := oleutil.CallMethod(scheduler, "Connect")
	if err != nil {
		return err
	}
	defer func() { _ = connected.Clear() }()
	folder, err := oleutil.CallMethod(scheduler, "GetFolder", `\`)
	if err != nil {
		return err
	}
	defer func() { _ = folder.Clear() }()
	if err := ctx.Err(); err != nil {
		return err
	}
	return action(folder.ToIDispatch())
}

func enabled(ctx context.Context) (bool, error) {
	var enabled bool
	err := withFolder(ctx, func(folder *ole.IDispatch) error {
		task, err := oleutil.CallMethod(folder, "GetTask", name)
		if err != nil {
			if isNotFound(err) {
				return nil
			}
			return err
		}
		defer func() { _ = task.Clear() }()
		value, err := oleutil.GetProperty(task.ToIDispatch(), "Enabled")
		if err != nil {
			return err
		}
		defer func() { _ = value.Clear() }()
		enabled = value.Val != 0
		return nil
	})
	return enabled, err
}

func enable(ctx context.Context) error {
	executablePath, err := os.Executable()
	if err != nil {
		return err
	}
	var command bytes.Buffer
	_ = xml.EscapeText(&command, []byte(executablePath))
	return withFolder(ctx, func(folder *ole.IDispatch) error {
		registeredTask, err := oleutil.CallMethod(folder, "RegisterTask", name, fmt.Sprintf(task, command.String()), 6, "", "", 3, "")
		if err != nil {
			return fmt.Errorf("enable autostart: %w", err)
		}
		defer func() { _ = registeredTask.Clear() }()
		return nil
	})
}

func disable(ctx context.Context) error {
	return withFolder(ctx, func(folder *ole.IDispatch) error {
		task, err := oleutil.CallMethod(folder, "DeleteTask", name, 0)
		if err != nil {
			if isNotFound(err) {
				return nil
			}
			return fmt.Errorf("disable autostart: %w", err)
		}
		defer func() { _ = task.Clear() }()
		return nil
	})
}
