package ipc

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

func peerPID(fd uintptr) (int, error) {
	// SIO_AF_UNIX_GETPEERPID from Windows SDK shared/afunix.h.
	const getPeerPID = windows.IOC_OUT | windows.IOC_VENDOR | 256
	var pid, size uint32
	err := windows.WSAIoctl(windows.Handle(fd), getPeerPID, nil, 0, (*byte)(unsafe.Pointer(&pid)), uint32(unsafe.Sizeof(pid)), &size, nil, 0)
	return int(pid), err
}
