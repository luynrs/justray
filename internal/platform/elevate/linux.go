//go:build linux

package elevate

import (
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

func Needed(err error) bool {
	if !errors.Is(err, os.ErrPermission) {
		return false
	}
	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	var capabilities [2]unix.CapUserData
	return unix.Capget(&header, &capabilities[0]) != nil || capabilities[0].Effective&(1<<unix.CAP_NET_ADMIN) == 0
}

func Restart(dir string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if hasNetAdmin(self) {
		return errors.New("CAP_NET_ADMIN is not effective for this process")
	}
	target, err := cachedCopy(self, dir)
	if err != nil {
		return err
	}

	if !hasNetAdmin(target) {
		elevate := "pkexec"
		if _, err := exec.LookPath(elevate); err != nil {
			elevate = "sudo"
		}
		if err := exec.Command(elevate, "setcap", "cap_net_admin+ep", target).Run(); err != nil {
			return errors.New("could not grant permissions")
		}
	}

	return syscall.Exec(target, os.Args, os.Environ())
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
