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
	"github.com/luynrs/justray/internal/domain"
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
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	dec := json.NewDecoder(c)
	var initial ipc.Snapshot
	if err := dec.Decode(&initial); err != nil {
		t.Fatalf("initial Watch snapshot: %v", err)
	}
	if initial.Settings.Port != domain.DefaultPort {
		t.Fatalf("incomplete initial snapshot: %+v", initial)
	}
	client := ipc.NewClient(ln.Addr().String())
	sub, err := client.AddSub("vless://11111111-1111-1111-1111-111111111111@127.0.0.1:443?security=tls#node")
	if err != nil || sub.ID == "" || sub.Nodes != 1 {
		t.Fatalf("AddSub result=%+v error=%v", sub, err)
	}
	var added ipc.Snapshot
	if err := dec.Decode(&added); err != nil {
		t.Fatal(err)
	}
	if len(added.Nodes) != 1 || added.Nodes[0].Sub != sub.ID {
		t.Fatalf("subscription was not pushed: %+v", added)
	}
	if err := client.SetTun(true); err != nil {
		t.Fatal(err)
	}
	var changed ipc.Snapshot
	if err := dec.Decode(&changed); err != nil {
		t.Fatal(err)
	}
	if !changed.Status.Tun {
		t.Fatalf("mode was not pushed: %+v", changed)
	}
	if result, err := srv.dispatch(context.Background(), ipc.Req{Method: "SetTun"}); err != nil || result != nil {
		t.Fatalf("command returned an unnecessary snapshot: result=%v error=%v", result, err)
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
