package ipc

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
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

func Socket(dir string) string    { return filepath.Join(dir, "ipc", "justrayd.sock") }
func DaemonLog(dir string) string { return filepath.Join(dir, "logs", "daemon.log") }
func EngineLog(dir string) string { return filepath.Join(dir, "logs", "engine.log") }
func TUILog(dir string) string    { return filepath.Join(dir, "logs", "tui.log") }
func Config(dir string) string    { return filepath.Join(dir, "config.json") }
func State(dir string) string     { return filepath.Join(dir, "state.json") }

func ClearLog(path string) error {
	if err := os.Truncate(path, 0); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func Chown(path string) error {
	if runtime.GOOS != "darwin" || os.Geteuid() != 0 {
		return nil
	}
	uid, uidErr := strconv.Atoi(os.Getenv("JUSTRAY_UID"))
	gid, gidErr := strconv.Atoi(os.Getenv("JUSTRAY_GID"))
	if uidErr != nil || gidErr != nil || uid == 0 {
		return nil
	}
	return os.Chown(path, uid, gid)
}
