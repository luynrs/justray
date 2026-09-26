package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/luynrs/justray/internal/daemon/connection"
	"github.com/luynrs/justray/internal/daemon/core"
	"github.com/luynrs/justray/internal/daemon/store"
	"github.com/luynrs/justray/internal/daemon/subscription"
	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/engine"
	"github.com/luynrs/justray/internal/ipc"
	"github.com/luynrs/justray/internal/platform/elevate"
)

type switchEngine struct {
	running bool
	failB   error
	failTun error
}

func (instance *switchEngine) Apply(_ context.Context, spec engine.SessionSpec) error {
	if spec.Node.ID == "b" && instance.failB != nil {
		instance.running = false
		return instance.failB
	}
	if spec.Tun && instance.failTun != nil {
		instance.running = false
		return instance.failTun
	}
	instance.running = true
	return nil
}
func (instance *switchEngine) Stop() error   { instance.running = false; return nil }
func (instance *switchEngine) Running() bool { return instance.running }

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

func TestSwitch(t *testing.T) {
	directory := t.TempDir()
	disk := store.Disk{Dir: directory}
	if err := disk.Save(store.PersistentState{Subscriptions: []store.Subscription{{ID: "sub", Nodes: []domain.Node{
		{ID: "a", Name: "A", Server: "a.example"}, {ID: "b", Name: "B", Server: "b.example"},
	}}}}); err != nil {
		t.Fatal(err)
	}
	logger := log.New(io.Discard, "", 0)
	socket := filepath.Join(directory, "daemon.sock")
	client := ipc.NewClient(socket)
	a := domain.NodeRef{SubscriptionID: "sub", NodeID: "a"}
	b := domain.NodeRef{SubscriptionID: "sub", NodeID: "b"}
	tun := true
	start := func(failB, failTun error) (*core.Core, func()) {
		app, err := core.New(disk, connection.New(t.Context(), directory, func(context.Context, string) engine.Engine {
			return &switchEngine{failB: failB, failTun: failTun}
		}, nil, logger), subscription.New(t.Context(), logger))
		if err != nil {
			t.Fatal(err)
		}
		if err := app.Restore(); err != nil {
			t.Fatal(err)
		}
		listener, err := net.Listen("unix", socket)
		if err != nil {
			t.Fatal(err)
		}
		server := New(t.Context(), logger, app)
		done := make(chan error, 1)
		go func() { done <- server.Serve(listener); close(done) }()
		stop := func() {
			server.Shutdown()
			if err, ok := <-done; ok && err != nil {
				t.Error(err)
			}
			app.Shutdown()
		}
		t.Cleanup(stop)
		return app, stop
	}
	check := func(ref domain.NodeRef, mode, pending bool) {
		state, err := disk.Load()
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := client.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		if state.Active != ref || state.Last != ref || state.Tun != mode || (state.Pending != nil) != pending ||
			snapshot.Selected != ref || !pending && (!snapshot.Status.Connected || snapshot.Status.NodeRef != ref || snapshot.Status.Tun != mode) {
			t.Fatalf("state=%+v snapshot=%+v", state, snapshot)
		}
	}
	failure := errors.New("switch failed")
	_, stop := start(failure, failure)
	if err := client.Connect(a, nil); err != nil {
		t.Fatal(err)
	}
	if err := client.Connect(b, &tun); err == nil {
		t.Fatal("switch to B succeeded")
	}
	check(a, false, false)
	if err := client.SetTun(true); err == nil {
		t.Fatal("TUN switch succeeded")
	}
	check(a, false, false)
	stop()

	permission := errors.Join(os.ErrPermission, errors.New("operation not permitted"))
	if !elevate.Needed(permission) {
		return
	}
	app, stop := start(permission, permission)
	check(a, false, false)
	if err := client.Connect(b, &tun); err == nil || err.Error() != ipc.ErrElevate.Error() {
		t.Fatalf("elevation request: %v", err)
	}
	check(a, false, true)
	state, err := disk.Load()
	if err != nil || state.Pending.Ref != b || !state.Pending.Tun {
		t.Fatalf("pending B: %+v, %v", state, err)
	}
	select {
	case <-app.RestartRequested():
	default:
		t.Fatal("daemon did not request elevation")
	}
	stop()

	_, stop = start(permission, permission)
	check(a, false, false)
	if err := client.SetTun(true); err == nil || err.Error() != ipc.ErrElevate.Error() {
		t.Fatalf("mode elevation request: %v", err)
	}
	check(a, false, true)
	state, err = disk.Load()
	if err != nil || state.Pending.Ref != a || !state.Pending.Tun {
		t.Fatalf("pending TUN: %+v, %v", state, err)
	}
	stop()

	_, stop = start(permission, permission)
	check(a, false, false)
	if err := client.Connect(b, &tun); err == nil || err.Error() != ipc.ErrElevate.Error() {
		t.Fatalf("elevation request: %v", err)
	}
	stop()

	_, _ = start(nil, nil)
	check(b, true, false)
}
