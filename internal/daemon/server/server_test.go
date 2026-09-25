package server

import (
	"encoding/json"
	"io"
	"log"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/luynrs/justray/internal/daemon/connection"
	"github.com/luynrs/justray/internal/daemon/core"
	"github.com/luynrs/justray/internal/daemon/store"
	"github.com/luynrs/justray/internal/daemon/subscription"
	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/ipc"
)

func TestIPCWatchLifecycle(t *testing.T) {
	directory := t.TempDir()
	listener, err := net.Listen("unix", filepath.Join(directory, "daemon.sock"))
	if err != nil {
		t.Fatal(err)
	}
	logger := log.New(io.Discard, "", 0)
	app, err := core.New(store.Disk{Dir: directory}, connection.New(t.Context(), directory, nil, nil, logger), subscription.New(t.Context(), logger))
	if err != nil {
		t.Fatal(err)
	}
	server := New(t.Context(), logger, app)
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()

	watchConn, err := net.Dial("unix", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = watchConn.Close() }()
	if err := json.NewEncoder(watchConn).Encode(ipc.Req{Method: "Watch"}); err != nil {
		t.Fatal(err)
	}
	_ = watchConn.SetReadDeadline(time.Now().Add(3 * time.Second))
	decoder := json.NewDecoder(watchConn)
	var snapshot ipc.Snapshot
	if err := decoder.Decode(&snapshot); err != nil || snapshot.Settings.Port != domain.DefaultPort {
		t.Fatalf("initial snapshot: %+v, %v", snapshot, err)
	}

	client := ipc.NewClient(listener.Addr().String())
	added, err := client.AddSub("vless://11111111-1111-1111-1111-111111111111@127.0.0.1:443?security=tls#node")
	if err != nil || added.ID == "" || added.Nodes != 1 {
		t.Fatalf("added subscription: %+v, %v", added, err)
	}
	if err := decoder.Decode(&snapshot); err != nil || len(snapshot.Nodes) != 1 || snapshot.Nodes[0].Sub != added.ID {
		t.Fatalf("subscription snapshot: %+v, %v", snapshot, err)
	}
	if err := client.SetTun(true); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(&snapshot); err != nil || !snapshot.Status.Tun {
		t.Fatalf("TUN snapshot: %+v, %v", snapshot, err)
	}

	shutdownDone := make(chan struct{})
	go func() { server.Shutdown(); close(shutdownDone) }()
	select {
	case <-shutdownDone:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not close Watch")
	}
	if err := <-serveDone; err != nil {
		t.Fatal(err)
	}
}
