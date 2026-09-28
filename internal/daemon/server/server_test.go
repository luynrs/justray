package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
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
	link := "vless://11111111-1111-1111-1111-111111111111@127.0.0.1:443?security=tls&type=xhttp&extra=" + url.QueryEscape(`{"serverMaxHeaderBytes":16384,"sessionIDPlacement":"query"}`)
	added, err := client.AddSub(t.Context(), link)
	if err != nil || added.ID == "" || added.Nodes != 1 {
		t.Fatalf("added subscription: %+v, %v", added, err)
	}
	if err := decoder.Decode(&snapshot); err != nil || len(snapshot.Nodes) != 1 || snapshot.Nodes[0].Sub != added.ID {
		t.Fatalf("subscription snapshot: %+v, %v", snapshot, err)
	}
	if _, err := client.AddSub(t.Context(), link); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(&snapshot); err != nil || len(snapshot.Nodes) != 2 || snapshot.Nodes[0].ID == snapshot.Nodes[1].ID {
		t.Fatalf("duplicate link snapshot: %+v, %v", snapshot, err)
	}
	if err := client.SetTun(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(&snapshot); err != nil || !snapshot.Status.Tun {
		t.Fatalf("TUN snapshot: %+v, %v", snapshot, err)
	}
	settings := snapshot.Settings
	settings.DNS = "1.1.1.1"
	if err := client.SetSettings(t.Context(), settings); err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(ipc.Config(directory))
	if err != nil || strings.Contains(string(config), `"autostart"`) {
		t.Fatalf("autostart stored in config: %s, %v", config, err)
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

func TestSubscriptionRefresh(t *testing.T) {
	var body atomic.Value
	first, second := "trojan://secret@example.com:443#first", "trojan://secret@example.com:443#second"
	body.Store(first + "\n" + second)
	source := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(response, body.Load().(string))
	}))
	defer source.Close()

	directory := t.TempDir()
	disk := store.Disk{Dir: directory}
	logger := log.New(io.Discard, "", 0)
	app, err := core.New(disk, connection.New(t.Context(), directory, nil, nil, logger), subscription.New(t.Context(), logger))
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", filepath.Join(directory, "daemon.sock"))
	if err != nil {
		t.Fatal(err)
	}
	server := New(t.Context(), logger, app)
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	defer func() {
		server.Shutdown()
		if err := <-serveDone; err != nil {
			t.Error(err)
		}
	}()

	client := ipc.NewClient(listener.Addr().String())
	added, err := client.AddSub(t.Context(), source.URL)
	if err != nil {
		t.Fatal(err)
	}
	before, err := disk.Load()
	if err != nil {
		t.Fatal(err)
	}
	if nodes := before.Subscriptions[0].Nodes; len(nodes) != 2 || nodes[0].ID == nodes[1].ID {
		t.Fatalf("duplicate links were merged or given the same ID: %d nodes", len(nodes))
	}
	for name, content := range map[string]string{
		"clash":              "proxies:\n  - {name: valid, type: trojan, server: example.com, port: 443, password: secret}\n  - {name: bad, type: tuic, server: example.com, port: 443, password: secret}",
		"clash malformed":    "proxies:\n  - {name: bad, type: trojan, server: example.com, port: [}\n",
		"sing-box null":      `{"outbounds":[{"type":"trojan","server":"example.com","server_port":443,"password":"secret"},null]}`,
		"sing-box no type":   `{"outbounds":[{"type":"trojan","server":"example.com","server_port":443,"password":"secret"},{"tag":"missing"}]}`,
		"sing-box unknown":   `{"outbounds":[{"type":"trojan","server":"example.com","server_port":443,"password":"secret"},{"type":"future-proxy","tag":"unknown"}]}`,
		"sing-box bad":       `{"outbounds":[{"type":"trojan","server":"example.com","server_port":443,"password":"secret"},{"type":"vless","server":"example.com","server_port":"bad"}]}`,
		"sing-box transport": `{"outbounds":[{"type":"shadowsocks","server":"example.com","server_port":443,"method":"aes-128-gcm","password":"secret","transport":{"type":"ws"}}]}`,
		"shadowtls detour":   `{"outbounds":[{"type":"shadowtls","tag":"stls","server":"example.com","server_port":443},{"type":"shadowsocks","server":"example.com","server_port":8388,"method":"aes-128-gcm","password":"secret","detour":"stls"}]}`,
		"xray null":          `{"outbounds":[{"protocol":"trojan","settings":{"servers":[{"address":"example.com","port":443,"password":"secret"}]}},null]}`,
		"xray no protocol":   `{"outbounds":[{"protocol":"trojan","settings":{"servers":[{"address":"example.com","port":443,"password":"secret"}]}},{"tag":"missing"}]}`,
		"xray unknown":       `{"outbounds":[{"protocol":"trojan","settings":{"servers":[{"address":"example.com","port":443,"password":"secret"}]}},{"protocol":"future-proxy","tag":"unknown"}]}`,
		"xray bad":           `{"outbounds":[{"protocol":"trojan","settings":{"servers":[{"address":"example.com","port":443,"password":"secret"}]}},{"protocol":"vless","settings":{"vnext":[{"address":"example.com","port":"bad","users":[{"id":"uuid"}]}]}}]}`,
		"xray incompatible":  `{"outbounds":[{"protocol":"vless","settings":{"vnext":[{"address":"example.com","port":443,"users":[{"id":"uuid"}]}],"servers":[{"address":"ignored.example","port":443,"password":"secret"}]}}]}`,
		"truncated json":     `{"outbounds":[{"type":"vless","server":"example.com"`,
	} {
		t.Run(name, func(t *testing.T) {
			body.Store(content)
			if err := client.Refresh(t.Context(), added.ID); err == nil || strings.Contains(err.Error(), "secret") {
				t.Fatalf("refresh error: %v", err)
			}
			after, err := disk.Load()
			if err != nil || !reflect.DeepEqual(after.Subscriptions, before.Subscriptions) {
				t.Fatalf("failed refresh changed subscriptions: before=%+v after=%+v err=%v", before.Subscriptions, after.Subscriptions, err)
			}
		})
	}
	body.Store(second + "\n" + first)
	if err := client.Refresh(t.Context(), added.ID); err != nil {
		t.Fatal(err)
	}
	after, err := disk.Load()
	if err != nil {
		t.Fatal(err)
	}
	old, current := before.Subscriptions[0].Nodes, after.Subscriptions[0].Nodes
	if len(current) != 2 || current[0].ID != old[1].ID || current[1].ID != old[0].ID {
		t.Fatal("refresh did not preserve separate IDs across reordering")
	}
	shadowsocks := "ss://" + base64.StdEncoding.EncodeToString([]byte("aes-256-gcm:pa%2Fss@word@example.com:8388")) + "#ss"
	body.Store(base64.StdEncoding.EncodeToString([]byte(first + "\n" + shadowsocks + "\nv2rayn://hysteria2/opaque\nvless://uuid@example.com:443?type=quic")))
	if err := client.Refresh(t.Context(), added.ID); err != nil {
		t.Fatalf("mixed subscription refresh: %v", err)
	}
	partial, err := disk.Load()
	if err != nil {
		t.Fatal(err)
	}
	if nodes := partial.Subscriptions[0].Nodes; len(nodes) != 2 || nodes[0].Name != "first" || nodes[1].Protocol != domain.SS || nodes[1].Server != "example.com" || nodes[1].Auth.Password != "pa%2Fss@word" {
		t.Fatalf("mixed refresh did not save the supported node: %+v", nodes)
	}
	if warning := partial.Subscriptions[0].Warning; warning != "skipped nodes: 2 (invalid: 1, unsupported: 1)" {
		t.Fatalf("partial refresh did not persist a useful warning: %q", warning)
	}
	if snapshot, err := client.Snapshot(t.Context()); err != nil || snapshot.Subscriptions[0].Warning != partial.Subscriptions[0].Warning {
		t.Fatalf("partial refresh warning is missing from IPC: %+v, %v", snapshot.Subscriptions, err)
	}
	body.Store("vless://uuid@example.com:443?type=quic")
	if err := client.Refresh(t.Context(), added.ID); err == nil || !strings.Contains(err.Error(), "skipped nodes: 1 (invalid: 1)") {
		t.Fatalf("unusable-only refresh: %v", err)
	}
	unchanged, err := disk.Load()
	if err != nil || !reflect.DeepEqual(unchanged.Subscriptions, partial.Subscriptions) {
		t.Fatalf("unusable-only refresh changed subscriptions: %v", err)
	}
	body.Store(first)
	if err := client.Refresh(t.Context(), added.ID); err != nil {
		t.Fatal(err)
	}
	clean, err := disk.Load()
	if err != nil || clean.Subscriptions[0].Warning != "" {
		t.Fatalf("clean refresh kept the old warning: %+v, %v", clean.Subscriptions, err)
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
		snapshot, err := client.Snapshot(t.Context())
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
	if err := client.Connect(t.Context(), a, nil); err != nil {
		t.Fatal(err)
	}
	if err := client.Connect(t.Context(), b, &tun); err == nil {
		t.Fatal("switch to B succeeded")
	}
	check(a, false, false)
	if err := client.SetTun(t.Context(), true); err == nil {
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
	if err := client.Connect(t.Context(), b, &tun); err == nil || err.Error() != ipc.ErrElevate.Error() {
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
	if err := client.SetTun(t.Context(), true); err == nil || err.Error() != ipc.ErrElevate.Error() {
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
	if err := client.Connect(t.Context(), b, &tun); err == nil || err.Error() != ipc.ErrElevate.Error() {
		t.Fatalf("elevation request: %v", err)
	}
	stop()

	_, _ = start(nil, nil)
	check(b, true, false)
}
