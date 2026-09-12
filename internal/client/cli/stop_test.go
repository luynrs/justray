package cli

import (
	"context"
	"errors"
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
	if err := waitStopped(context.Background(), sock, 10*time.Millisecond); err == nil {
		t.Fatal("reported stopped while lock is held")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitStopped(ctx, sock, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}

	unlock()
	unlock = nil
	if err := waitStopped(context.Background(), sock, time.Second); err != nil {
		t.Fatal(err)
	}
}
