package ipc

import (
	"errors"
	"net"
	"os"
)

func killPeer(conn *net.UnixConn) error {
	raw, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	var pid int
	var peerErr error
	if err := raw.Control(func(fd uintptr) { pid, peerErr = peerPID(fd) }); err != nil {
		return err
	}
	if peerErr != nil {
		return peerErr
	}
	if pid <= 0 {
		return errors.New("cannot determine daemon PID")
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	defer func() { _ = process.Release() }()
	if err := process.Kill(); !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}
