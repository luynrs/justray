//go:build windows

package detach

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

func init() {
	windows.SetConsoleOutputCP(65001) // utf8
}

func Cmd(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NO_WINDOW | windows.DETACHED_PROCESS | syscall.CREATE_NEW_PROCESS_GROUP,
	}
}
