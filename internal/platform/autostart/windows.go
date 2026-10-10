//go:build windows

package autostart

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"

	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
	"golang.org/x/sys/windows"
)

const name = "justrayd"

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

func withFolder(ctx context.Context, action func(scheduler, folder *ole.IDispatch) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	type outcome struct {
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if err := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED); err != nil {
			if comError, ok := errors.AsType[*ole.OleError](err); !ok || comError.Code() != uintptr(windows.S_FALSE) {
				done <- outcome{err: err}
				return
			}
		}
		defer ole.CoUninitialize()
		object, err := oleutil.CreateObject("Schedule.Service")
		if err != nil {
			done <- outcome{err: err}
			return
		}
		defer object.Release()
		scheduler, err := object.QueryInterface(ole.IID_IDispatch)
		if err != nil {
			done <- outcome{err: err}
			return
		}
		defer scheduler.Release()
		connected, err := oleutil.CallMethod(scheduler, "Connect")
		if err != nil {
			done <- outcome{err: err}
			return
		}
		defer func() { _ = connected.Clear() }()
		folder, err := oleutil.CallMethod(scheduler, "GetFolder", `\`)
		if err != nil {
			done <- outcome{err: err}
			return
		}
		defer func() { _ = folder.Clear() }()
		if err := ctx.Err(); err != nil {
			done <- outcome{err: err}
			return
		}
		done <- outcome{err: action(scheduler, folder.ToIDispatch())}
	}()
	select {
	case <-ctx.Done():
		<-done
		return ctx.Err()
	case item := <-done:
		if err := ctx.Err(); err != nil {
			return err
		}
		return item.err
	}
}

func Enabled(ctx context.Context) (bool, error) {
	var enabled bool
	err := withFolder(ctx, func(_, folder *ole.IDispatch) error {
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
	if err != nil {
		return false, err
	}
	return enabled, err
}

func Enable(ctx context.Context) error {
	executablePath, err := os.Executable()
	if err != nil {
		return err
	}
	return withFolder(ctx, func(scheduler, folder *ole.IDispatch) error {
		taskDefinition, err := oleutil.CallMethod(scheduler, "NewTask", 0)
		if err != nil {
			return err
		}
		defer func() { _ = taskDefinition.Clear() }()
		taskDefinitionDispatch := taskDefinition.ToIDispatch()

		settings, err := oleutil.GetProperty(taskDefinitionDispatch, "Settings")
		if err != nil {
			return err
		}
		defer func() { _ = settings.Clear() }()
		settingsDispatch := settings.ToIDispatch()
		if _, err := oleutil.PutProperty(settingsDispatch, "DisallowStartIfOnBatteries", false); err != nil {
			return err
		}
		if _, err := oleutil.PutProperty(settingsDispatch, "StopIfGoingOnBatteries", false); err != nil {
			return err
		}
		if _, err := oleutil.PutProperty(settingsDispatch, "ExecutionTimeLimit", "PT0S"); err != nil {
			return err
		}

		principal, err := oleutil.GetProperty(taskDefinitionDispatch, "Principal")
		if err != nil {
			return err
		}
		defer func() { _ = principal.Clear() }()
		principalDispatch := principal.ToIDispatch()
		if _, err := oleutil.PutProperty(principalDispatch, "LogonType", 3); err != nil {
			return err
		}
		if _, err := oleutil.PutProperty(principalDispatch, "RunLevel", 1); err != nil {
			return err
		}

		triggers, err := oleutil.GetProperty(taskDefinitionDispatch, "Triggers")
		if err != nil {
			return err
		}
		defer func() { _ = triggers.Clear() }()
		trigger, err := oleutil.CallMethod(triggers.ToIDispatch(), "Create", 9)
		if err != nil {
			return err
		}
		defer func() { _ = trigger.Clear() }()

		actions, err := oleutil.GetProperty(taskDefinitionDispatch, "Actions")
		if err != nil {
			return err
		}
		defer func() { _ = actions.Clear() }()
		action, err := oleutil.CallMethod(actions.ToIDispatch(), "Create", 0)
		if err != nil {
			return err
		}
		defer func() { _ = action.Clear() }()
		if _, err := oleutil.PutProperty(action.ToIDispatch(), "Path", executablePath); err != nil {
			return err
		}

		if err := ctx.Err(); err != nil {
			return err
		}
		registeredTask, err := oleutil.CallMethod(folder, "RegisterTaskDefinition", name, taskDefinitionDispatch, 6, "", "", 3, "")
		if err != nil {
			return fmt.Errorf("enable autostart: %w", err)
		}
		defer func() { _ = registeredTask.Clear() }()
		return nil
	})
}

func Disable(ctx context.Context) error {
	return withFolder(ctx, func(_, folder *ole.IDispatch) error {
		if err := ctx.Err(); err != nil {
			return err
		}
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
