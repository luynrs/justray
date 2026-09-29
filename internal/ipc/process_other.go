//go:build !linux && !darwin && !windows

package ipc

import "errors"

func peerPID(uintptr) (int, error) {
	return 0, errors.New("stopping an unversioned daemon is unsupported on this platform")
}
