//go:build windows

package wintun

import (
	"crypto/sha256"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"golang.org/x/sys/windows"
)

//go:embed wintun_amd64.dll wintun_arm64.dll
var blobs embed.FS

var names = map[string]string{
	"amd64": "wintun_amd64.dll",
	"arm64": "wintun_arm64.dll",
}

func Ensure() (string, error) {
	name, ok := names[runtime.GOARCH]
	if !ok {
		return "", nil
	}
	blob, err := blobs.ReadFile(name)
	if err != nil {
		return "", err
	}

	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	dst := filepath.Join(filepath.Dir(exe), "wintun.dll")
	want := sha256.Sum256(blob)
	if current, err := os.ReadFile(dst); err == nil && sha256.Sum256(current) == want {
		return dst, nil
	}
	tmpFile, err := os.CreateTemp(filepath.Dir(dst), ".wintun-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create temporary %s: %w", dst, err)
	}
	tmp := tmpFile.Name()
	defer func() { _ = os.Remove(tmp) }()
	if _, err := tmpFile.Write(blob); err != nil {
		_ = tmpFile.Close()
		return "", fmt.Errorf("write %s: %w", dst, err)
	}
	if err := tmpFile.Close(); err != nil {
		return "", fmt.Errorf("write %s: %w", dst, err)
	}
	from, err := windows.UTF16PtrFromString(tmp)
	if err != nil {
		return "", err
	}
	to, err := windows.UTF16PtrFromString(dst)
	if err != nil {
		return "", err
	}
	if err := windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH); err != nil {
		return "", fmt.Errorf("replace %s: %w", dst, err)
	}
	return dst, nil
}
