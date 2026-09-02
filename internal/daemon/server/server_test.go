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
	"github.com/luynrs/justray/internal/shared/rpc"
)

func TestListenDoesNotWaitForLock(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "daemon.sock")
	ln, unlock, err := Listen(socket)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = ln.Close()
		unlock()
	}()

	if _, _, err := Listen(socket); err == nil || !strings.Contains(err.Error(), "already listening") {
		t.Fatalf("second Listen error = %v", err)
	}
}

func TestShutdownClosesWatch(t *testing.T) {
	dir := t.TempDir()
	ln, err := net.Listen("unix", filepath.Join(dir, "daemon.sock"))
	if err != nil {
		t.Fatal(err)
	}
	logger := log.New(io.Discard, "", 0)
	st := store.Disk{Dir: dir}
	app, err := core.New(st, connection.New(context.Background(), dir, nil, nil, logger), subscription.New(context.Background(), logger))
	if err != nil {
		t.Fatal(err)
	}
	srv := New(context.Background(), logger, app)
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()

	client, err := net.Dial("unix", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := json.NewEncoder(client).Encode(rpc.Req{Method: "Watch"}); err != nil {
		t.Fatal(err)
	}
	_ = client.SetReadDeadline(time.Now().Add(time.Second))
	if err := json.NewDecoder(client).Decode(&rpc.Changed{}); err != nil {
		t.Fatalf("initial Watch revision: %v", err)
	}

	stopped := make(chan struct{})
	go func() {
		srv.Shutdown()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Shutdown did not wait for Watch to exit")
	}
	if err := <-served; err != nil {
		t.Fatalf("Serve: %v", err)
	}
}
