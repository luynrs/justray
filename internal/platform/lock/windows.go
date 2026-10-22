//go:build windows

package lock

import (
	"os"

	"golang.org/x/sys/windows"
)

func Open(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, new(windows.Overlapped)); err != nil {
		_ = file.Close()
		if err == windows.ERROR_LOCK_VIOLATION {
			return nil, ErrLocked
		}
		return nil, err
	}
	return file, nil
}

func Close(file *os.File) {
	_ = windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, new(windows.Overlapped))
	_ = file.Close()
}
