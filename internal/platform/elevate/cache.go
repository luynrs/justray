//go:build linux

package elevate

import (
	"io"
	"os"
	"path/filepath"
)

func Executable(source, dir string) string {
	target := filepath.Join(dir, "elevated", "justrayd")
	if sum, err := hashFile(source); err == nil && hasNetAdmin(target) && verified(target, sum) {
		return target
	}
	return source
}

func cachedCopy(dir string) (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	sum, err := hashFile(self)
	if err != nil {
		return "", err
	}

	target := filepath.Join(dir, "elevated", "justrayd")
	if verified(target, sum) {
		return target, nil
	}
	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return "", err
	}
	return target, copyFile(self, target)
}

func verified(path, sum string) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	got, err := hashFile(path)
	return err == nil && got == sum
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o700)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return err
	}
	return out.Close()
}
