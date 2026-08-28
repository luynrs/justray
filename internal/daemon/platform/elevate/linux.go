//go:build linux

package elevate

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

func Needed(err error) bool {
	self, _ := os.Executable()
	return err != nil && errors.Is(err, os.ErrPermission) && !hasNetAdmin(self)
}

func Restart(dir string) error {
	target, err := cachedCopy(dir)
	if err != nil {
		return err
	}

	if !hasNetAdmin(target) {
		elevate := "pkexec"
		if _, err := exec.LookPath(elevate); err != nil {
			elevate = "sudo"
		}
		if out, err := exec.Command(elevate, "setcap", "cap_net_admin+ep", target).CombinedOutput(); err != nil {
			return fmt.Errorf("%v: %s", err, out)
		}
	}

	cmd := exec.Command(target, os.Args[1:]...)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func hasNetAdmin(path string) bool {
	buf := make([]byte, 32) // fits VFS_CAP_REVISION_3 (24 bytes)
	n, err := syscall.Getxattr(path, "security.capability", buf)
	if err != nil || n < 8 {
		return false
	}
	if binary.LittleEndian.Uint32(buf[0:4])&0x1 == 0 {
		return false
	}
	return binary.LittleEndian.Uint32(buf[4:8])&(1<<12) != 0 // CAP_NET_ADMIN
}
