//go:build windows

package autostart

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestTask(t *testing.T) {
	if os.Getenv("JUSTRAY_TEST_SCHEDULER") != "1" {
		t.Skip("set JUSTRAY_TEST_SCHEDULER=1 to register a temporary task")
	}
	name := fmt.Sprintf("justray-test-%d", os.Getpid())
	t.Cleanup(func() { _ = deleteTask(name) })
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	var bin bytes.Buffer
	_ = xml.EscapeText(&bin, []byte(filepath.Join(dir, "justray & тест.exe")))
	xmlPath := filepath.Join(dir, "task.xml")
	if err := os.WriteFile(xmlPath, fmt.Appendf(nil, task, u.User.Sid.String(), bin.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := cmd("/Create", "/TN", name, "/XML", xmlPath).CombinedOutput(); err != nil {
		t.Fatalf("create: %v: %s", err, out)
	}
	if err := cmd("/Query", "/TN", name).Run(); err != nil {
		t.Fatalf("query: %v", err)
	}
	for range 2 {
		if err := deleteTask(name); err != nil {
			t.Fatal(err)
		}
	}
	if err := deleteTask(""); err == nil {
		t.Fatal("invalid task name reported deleted")
	}
}
