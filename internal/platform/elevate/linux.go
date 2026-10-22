//go:build linux

package elevate

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"

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

func Restart(dir string, reservation *os.File) error {
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
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := exec.CommandContext(ctx, elevate, "setcap", "cap_net_admin+ep", target).Run(); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return errors.New("could not grant permissions")
		}
	}

	syscall.ForkLock.RLock()
	defer syscall.ForkLock.RUnlock()
	if _, err := unix.FcntlInt(reservation.Fd(), unix.F_SETFD, 0); err != nil {
		return err
	}
	defer unix.CloseOnExec(int(reservation.Fd()))
	return syscall.Exec(target, os.Args, append(os.Environ(), "JUSTRAY_ELEVATION_FD="+strconv.Itoa(int(reservation.Fd()))))
}

func TakeReservation(path string) (*os.File, error) {
	raw := os.Getenv("JUSTRAY_ELEVATION_FD")
	_ = os.Unsetenv("JUSTRAY_ELEVATION_FD")
	if raw == "" {
		return nil, nil
	}
	descriptor, err := strconv.Atoi(raw)
	if err != nil || descriptor < 3 {
		return nil, errors.New("invalid inherited elevation lock descriptor")
	}
	reservation := os.NewFile(uintptr(descriptor), path)
	info, statErr := reservation.Stat()
	expected, pathErr := os.Stat(path)
	flags, flagErr := unix.FcntlInt(reservation.Fd(), unix.F_GETFL, 0)
	if statErr != nil || pathErr != nil || flagErr != nil || !info.Mode().IsRegular() || !os.SameFile(info, expected) || flags&unix.O_ACCMODE != unix.O_RDWR {
		_ = reservation.Close()
		return nil, errors.New("invalid inherited elevation lock")
	}
	if err := unix.Flock(descriptor, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = reservation.Close()
		return nil, err
	}
	unix.CloseOnExec(descriptor)
	return reservation, nil
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
	return binary.LittleEndian.Uint32(buf[4:8])&(1<<unix.CAP_NET_ADMIN) != 0 // CAP_NET_ADMIN
}
