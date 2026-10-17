//go:build linux

package elevate

import (
	"errors"
	"io"
	"os"
	"path/filepath"
)

func cachedCopy(source, dir string) (string, error) {
	sum, err := hashFile(source)
	if err != nil {
		return "", err
	}

	target := filepath.Join(dir, "elevated", "justrayd")
	if cachedInfo, err := os.Lstat(target); err == nil && cachedInfo.Mode().IsRegular() {
		if cachedSum, err := hashFile(target); err == nil && cachedSum == sum {
			return target, nil
		}
	}
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return "", err
	}
	sourceFile, err := os.Open(source)
	if err != nil {
		return target, err
	}
	defer func() { _ = sourceFile.Close() }()

	targetFile, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o700)
	if err != nil {
		return target, err
	}
	if _, err := io.Copy(targetFile, sourceFile); err != nil {
		_ = targetFile.Close()
		_ = os.Remove(target)
		return target, err
	}
	return target, targetFile.Close()
}
