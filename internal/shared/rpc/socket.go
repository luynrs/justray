package rpc

import (
	"os"
	"path/filepath"
)

func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "justray"), nil
}

func EnsureDir(dir string) error {
	for _, sub := range []string{"logs", "ipc"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o700); err != nil {
			return err
		}
	}
	return os.Chmod(dir, 0o700)
}

func Socket(dir string) string        { return filepath.Join(dir, "ipc", "justrayd.sock") }
func DaemonLog(dir string) string     { return filepath.Join(dir, "logs", "daemon.log") }
func EngineLog(dir string) string     { return filepath.Join(dir, "logs", "engine.log") }
func Subscriptions(dir string) string { return filepath.Join(dir, "subscriptions.yaml") }
func Configuration(dir string) string { return filepath.Join(dir, "configuration.yaml") }

func ClearLog(path string) error {
	if err := os.Truncate(path, 0); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
