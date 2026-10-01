//go:build !linux && !darwin && !windows

package elevate

func Executable(source, _ string) string { return source }

func Needed(error) bool { return false }

func Restart(string) error { return nil }
