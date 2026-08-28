//go:build windows

package autostart

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"
)

const name = "justrayd"

func schtasks() string {
	root, err := windows.GetSystemDirectory()
	if err != nil {
		root = `C:\Windows`
	}
	return filepath.Join(root, "schtasks.exe")
}

func task(args ...string) *exec.Cmd {
	cmd := exec.Command(schtasks(), args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	return cmd
}

func Enabled() bool {
	return task("/Query", "/TN", name).Run() == nil
}

func Enable() error {
	bin, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := task("/Create", "/F", "/RL", "LIMITED", "/SC", "ONLOGON", "/TN", name, "/TR", `"`+bin+`"`)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("schtasks create: %v: %s", err, out)
	}
	return nil
}

func Disable() error {
	_ = task("/Delete", "/F", "/TN", name).Run()
	return nil
}
