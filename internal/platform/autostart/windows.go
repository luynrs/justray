//go:build windows

package autostart

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"
)

const name = "justrayd"

const task = `<?xml version="1.0" encoding="UTF-8"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <Triggers><LogonTrigger><UserId>%[1]s</UserId></LogonTrigger></Triggers>
  <Principals><Principal id="User"><UserId>%[1]s</UserId><LogonType>InteractiveToken</LogonType><RunLevel>LeastPrivilege</RunLevel></Principal></Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <RestartOnFailure><Interval>PT1M</Interval><Count>3</Count></RestartOnFailure>
  </Settings>
  <Actions Context="User"><Exec><Command>%[2]s</Command></Exec></Actions>
</Task>
`

func schtasks() string {
	root, err := windows.GetSystemDirectory()
	if err != nil {
		root = `C:\Windows\System32`
	}
	return filepath.Join(root, "schtasks.exe")
}

func cmd(args ...string) *exec.Cmd {
	c := exec.Command(schtasks(), args...)
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
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	var command bytes.Buffer
	_ = xml.EscapeText(&command, []byte(bin))
	f, err := os.CreateTemp("", "justrayd-*.xml")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	_, err = fmt.Fprintf(f, task, user.User.Sid.String(), command.String())
	if err := errors.Join(err, f.Close()); err != nil {
		return err
	}
	if out, err := cmd("/Create", "/F", "/TN", name, "/XML", f.Name()).CombinedOutput(); err != nil {
		return fmt.Errorf("enable autostart: %w: %s", err, bytes.TrimSpace(out))
	}
	return nil
}

func Disable() error { return deleteTask(name) }

func deleteTask(name string) error {
	out, err := cmd("/Delete", "/F", "/TN", name).CombinedOutput()
	if err == nil {
		return nil
	}
	var exit *exec.ExitError
	if queryErr := cmd("/Query", "/TN", name, "/HRESULT").Run(); errors.As(queryErr, &exit) && uint32(exit.ExitCode()) == 0x80070002 {
		return nil // already absent
	}
	return fmt.Errorf("disable autostart: %w: %s", err, bytes.TrimSpace(out))
}
