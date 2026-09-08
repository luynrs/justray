package cli

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/luynrs/justray/internal/platform/lock"
)

func TestWaitStopped(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "daemon.sock")
	unlock, err := lock.File(sock + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if unlock != nil {
			unlock()
		}
	})
	if err := waitStopped(sock, 10*time.Millisecond); err == nil {
		t.Fatal("reported stopped while lock is held")
	}
	unlock()
	unlock = nil
	if err := waitStopped(sock, time.Second); err != nil {
		t.Fatal(err)
	}
}
