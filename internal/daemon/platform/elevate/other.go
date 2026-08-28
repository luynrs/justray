//go:build !linux && !darwin && !windows

package elevate

func Needed(error) bool { return false }

func Restart(string) error { return nil }
