//go:build windows

package elevate

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func Executable(source, _ string) string { return source }

func Needed(err error) bool {
	if err == nil || windows.GetCurrentProcessToken().IsElevated() {
		return false
	}
	return errors.Is(err, os.ErrPermission)
}

func Restart(_ string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}

	verb, _ := windows.UTF16PtrFromString("runas")
	file, _ := windows.UTF16PtrFromString(self)
	if err := windows.ShellExecute(0, verb, file, nil, nil, windows.SW_HIDE); err != nil {
		return errors.New("could not grant permissions")
	}
	return nil
}
