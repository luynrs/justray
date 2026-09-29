package ipc

import "golang.org/x/sys/unix"

func peerPID(fd uintptr) (int, error) {
	return unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERPID)
}
