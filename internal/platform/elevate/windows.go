//go:build windows

package elevate

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func Needed(err error) bool {
	if err == nil || windows.GetCurrentProcessToken().IsElevated() {
		return false
	}
	return errors.Is(err, os.ErrPermission)
}

func Restart(_ string, _ *os.File) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}

	verb, _ := windows.UTF16PtrFromString("runas")
	file, _ := windows.UTF16PtrFromString(self)
	args, _ := windows.UTF16PtrFromString(windows.ComposeCommandLine(os.Args[1:]))
	if err := windows.ShellExecute(0, verb, file, args, nil, windows.SW_HIDE); err != nil {
		return errors.New("could not grant permissions")
	}
	return nil
}

func TakeReservation(string) (*os.File, error) { return nil, nil }
