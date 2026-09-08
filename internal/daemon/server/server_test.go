package server

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/luynrs/justray/internal/daemon/connection"
	"github.com/luynrs/justray/internal/daemon/core"
	"github.com/luynrs/justray/internal/daemon/store"
	"github.com/luynrs/justray/internal/daemon/subscription"
	"github.com/luynrs/justray/internal/ipc"
)

func TestListenLocked(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "daemon.sock")
	ln, unlock, err := Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = ln.Close()
		unlock()
	}()

	if _, _, err := Listen(sock); err == nil || !strings.Contains(err.Error(), "already listening") {
		t.Fatalf("second Listen error = %v", err)
	}
}

func TestShutdownWatch(t *testing.T) {
	dir := t.TempDir()
	ln, err := net.Listen("unix", filepath.Join(dir, "daemon.sock"))
	if err != nil {
		t.Fatal(err)
	}
	l := log.New(io.Discard, "", 0)
	st := store.Disk{Dir: dir}
	app, err := core.New(st, connection.New(context.Background(), dir, nil, nil, l), subscription.New(context.Background(), l))
	if err != nil {
		t.Fatal(err)
	}
	srv := New(context.Background(), l, app)
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()

	c, err := net.Dial("unix", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	if err := json.NewEncoder(c).Encode(ipc.Req{Method: "Watch"}); err != nil {
		t.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(time.Second))
	if err := json.NewDecoder(c).Decode(&ipc.Changed{}); err != nil {
		t.Fatalf("initial Watch revision: %v", err)
	}

	done := make(chan struct{})
	go func() {
		srv.Shutdown()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Shutdown did not wait for Watch to exit")
	}
	if err := <-served; err != nil {
		t.Fatalf("Serve: %v", err)
	}
}
