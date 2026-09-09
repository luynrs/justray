package core

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/luynrs/justray/internal/daemon/connection"
	"github.com/luynrs/justray/internal/daemon/store"
	"github.com/luynrs/justray/internal/daemon/subscription"
	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/engine"
)

func TestRefreshLock(t *testing.T) {
	var calls atomic.Int32
	var start sync.Once
	started := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		start.Do(func() { close(started) })
		<-release
		_, _ = io.WriteString(w, "vless://11111111-1111-1111-1111-111111111111@example.com:443?security=tls#node")
	}))
	defer srv.Close()

	transport := http.DefaultTransport
	http.DefaultTransport = srv.Client().Transport
	defer func() { http.DefaultTransport = transport }()

	disk := store.Disk{Dir: t.TempDir()}
	if err := disk.Save(store.PersistentState{Subscriptions: []store.Subscription{{ID: "sub", URL: srv.URL}}}); err != nil {
		t.Fatal(err)
	}
	logger := log.New(io.Discard, "", 0)
	subs := subscription.New(context.Background(), logger)
	app, err := New(disk, connection.New(context.Background(), "", nil, nil, logger), subs)
	if err != nil {
		t.Fatal(err)
	}
	sub := app.current().Subscriptions[0]

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first := make(chan error, 1)
	go func() {
		_, err := app.refresh(ctx, sub)
		first <- err
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if snap := app.Snapshot(); len(snap.Subscriptions) == 0 || !snap.Subscriptions[0].Refreshing {
		t.Fatalf("expected sub to be refreshing, got %+v", snap.Subscriptions)
	}
	moved := make(chan error, 1)
	go func() { moved <- app.MoveSubscription("sub", 1) }()
	select {
	case err := <-moved:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("mutation blocked on refresh I/O")
	}

	time.AfterFunc(200*time.Millisecond, func() { close(release) })
	if _, err := app.refresh(ctx, sub); err != nil {
		t.Fatal(err)
	}
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if snap := app.Snapshot(); len(snap.Subscriptions) == 0 || snap.Subscriptions[0].Refreshing {
		t.Fatalf("expected sub not to be refreshing, got %+v", snap.Subscriptions)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("HTTP calls = %d, want 1", got)
	}
}

func TestFind(t *testing.T) {
	subs := []store.Subscription{
		{ID: "a", Nodes: []domain.Node{{ID: "0123456789abcdef"}}},
		{ID: "b", Nodes: []domain.Node{{ID: "0123fedcba987654"}}},
	}
	if _, _, err := find(subs, domain.NodeRef{NodeID: "ffff"}); err == nil || err.Error() != `node "ffff" not found` {
		t.Fatalf("missing node: %v", err)
	}
	if _, _, err := find(subs, domain.NodeRef{NodeID: "0123"}); err == nil || err.Error() != `ambiguous node ID "0123"` {
		t.Fatalf("ambiguous node: %v", err)
	}
	_, ref, err := find(subs, domain.NodeRef{NodeID: "01234567"})
	if err != nil || ref != (domain.NodeRef{SubscriptionID: "a", NodeID: "0123456789abcdef"}) {
		t.Fatalf("unique node: ref=%+v err=%v", ref, err)
	}
}

func TestMoveSubscription(t *testing.T) {
	disk := store.Disk{Dir: t.TempDir()}
	if err := disk.Save(store.PersistentState{Subscriptions: []store.Subscription{{ID: "a"}, {ID: "b"}}}); err != nil {
		t.Fatal(err)
	}
	logger := log.New(io.Discard, "", 0)
	app, err := New(disk, connection.New(context.Background(), "", nil, nil, logger), subscription.New(context.Background(), logger))
	if err != nil {
		t.Fatal(err)
	}
	_, changed, cancel := app.Watch()
	defer cancel()
	if err := app.MoveSubscription("a", 1); err != nil {
		t.Fatal(err)
	}
	select {
	case up := <-changed:
		if !reflect.DeepEqual(up, app.Snapshot()) || len(up.Subscriptions) != 2 || up.Subscriptions[0].ID != "b" || up.Subscriptions[1].ID != "a" {
			t.Fatalf("unexpected subscription order: %+v", up)
		}
	case <-time.After(time.Second):
		t.Fatal("watch did not receive mutation snapshot")
	}
	state, err := disk.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := state.Subscriptions; len(got) != 2 || got[0].ID != "b" || got[1].ID != "a" {
		t.Fatalf("subscriptions = %+v", got)
	}
}

func TestRefreshSnapshot(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "vless://11111111-1111-1111-1111-111111111111@example.com:443?security=tls#node")
	}))
	defer srv.Close()
	transport := http.DefaultTransport
	http.DefaultTransport = srv.Client().Transport
	defer func() { http.DefaultTransport = transport }()

	disk := store.Disk{Dir: t.TempDir()}
	if err := disk.Save(store.PersistentState{Subscriptions: []store.Subscription{{ID: "sub", URL: srv.URL}}}); err != nil {
		t.Fatal(err)
	}
	logger := log.New(io.Discard, "", 0)
	app, err := New(disk, connection.New(context.Background(), "", nil, nil, logger), subscription.New(context.Background(), logger))
	if err != nil {
		t.Fatal(err)
	}
	_, changed, cancel := app.Watch()
	defer cancel()
	if err := app.RefreshSubscription(context.Background(), "sub"); err != nil {
		t.Fatal(err)
	}
	select {
	case up := <-changed:
		snap := app.Snapshot()
		if !reflect.DeepEqual(up, snap) || len(snap.Nodes) != 1 || snap.Subscriptions[0].Refreshing {
			t.Fatalf("update=%+v snap=%+v", up, snap)
		}
	case <-time.After(time.Second):
		t.Fatal("watch did not receive refresh snapshot")
	}
}

func TestProbe(t *testing.T) {
	disk := store.Disk{Dir: t.TempDir()}
	if err := disk.Save(store.PersistentState{Subscriptions: []store.Subscription{{ID: "sub", Nodes: []domain.Node{{ID: "node"}}}}}); err != nil {
		t.Fatal(err)
	}
	logger := log.New(io.Discard, "", 0)
	probe := func(_ context.Context, _ []domain.Node, _ domain.Settings, _ string, onResult func(string, engine.Result)) error {
		onResult("node", engine.Result{Alive: true, MS: 12})
		return nil
	}
	app, err := New(disk, connection.New(context.Background(), "", nil, probe, logger), subscription.New(context.Background(), logger))
	if err != nil {
		t.Fatal(err)
	}
	_, changed, cancel := app.Watch()
	defer cancel()
	if err := app.Probe(context.Background(), "sub", "node"); err != nil {
		t.Fatal(err)
	}
	select {
	case up := <-changed:
		snap := app.Snapshot()
		if !reflect.DeepEqual(up, snap) || len(snap.Nodes) != 1 || !snap.Nodes[0].Probed || !snap.Nodes[0].Alive || snap.Nodes[0].MS != 12 {
			t.Fatalf("update=%+v snap=%+v", up, snap)
		}
	case <-time.After(time.Second):
		t.Fatal("watch did not receive probe snapshot")
	}
}

func TestSetTun(t *testing.T) {
	disk := store.Disk{Dir: t.TempDir()}
	logger := log.New(io.Discard, "", 0)
	app, err := New(disk, connection.New(context.Background(), "", nil, nil, logger), subscription.New(context.Background(), logger))
	if err != nil {
		t.Fatal(err)
	}
	_, changed, cancel := app.Watch()
	defer cancel()
	if err := app.SetTun(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	select {
	case up := <-changed:
		snap := app.Snapshot()
		if !reflect.DeepEqual(up, snap) || !snap.Status.Tun {
			t.Fatalf("update=%+v snap=%+v", up, snap)
		}
	case <-time.After(time.Second):
		t.Fatal("watch did not receive TUN snapshot")
	}
	state, err := disk.Load()
	if err != nil || !state.Tun {
		t.Fatalf("state=%+v err=%v", state, err)
	}
}

type fakeEngine struct {
	closeErr error
	stopped  bool
}

func (e *fakeEngine) Apply(context.Context, engine.SessionSpec) error { return nil }
func (e *fakeEngine) Stop() error                                     { e.stopped = true; return e.closeErr }
func (e *fakeEngine) Running() bool                                   { return !e.stopped }

func testCore(t testing.TB, eng engine.Engine, state store.PersistentState) *Core {
	t.Helper()
	disk := store.Disk{Dir: t.TempDir()}
	if err := disk.Save(state); err != nil {
		t.Fatal(err)
	}
	logger := log.New(io.Discard, "", 0)
	conn := connection.New(context.Background(), "", func(context.Context, string) engine.Engine { return eng }, nil, logger)
	app, err := New(disk, conn, subscription.New(context.Background(), logger))
	if err != nil {
		t.Fatal(err)
	}
	return app
}

func TestStatusPort(t *testing.T) {
	settings, _ := domain.Settings{}.Normalize()
	settings.Port = 1080
	app := testCore(t, &fakeEngine{}, store.PersistentState{
		Settings:      settings,
		Subscriptions: []store.Subscription{{ID: "sub", Nodes: []domain.Node{{ID: "n1"}}}},
	})
	if st := app.Snapshot().Status; st.Connected || st.Port != 1080 {
		t.Fatalf("want disconnected port 1080, got %+v", st)
	}
	if err := app.Connect(context.Background(), "n1", "sub"); err != nil {
		t.Fatal(err)
	}
	if st := app.Snapshot().Status; !st.Connected || st.Port != 1080 {
		t.Fatalf("want connected port 1080, got %+v", st)
	}
	if err := app.Disconnect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if st := app.Snapshot().Status; st.Connected || st.Port != 1080 {
		t.Fatalf("want disconnected port 1080, got %+v", st)
	}
}

func TestContextCancelled(t *testing.T) {
	settings, _ := domain.Settings{}.Normalize()
	app := testCore(t, &fakeEngine{}, store.PersistentState{
		Settings:      settings,
		Subscriptions: []store.Subscription{{ID: "sub", Nodes: []domain.Node{{ID: "n1"}}}},
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, fn := range []func() error{
		func() error { return app.Connect(ctx, "n1", "sub") },
		func() error { return app.Disconnect(ctx) },
		func() error { return app.SetTun(ctx, true) },
		func() error { return app.SetSettings(ctx, settings) },
		func() error { _, err := app.AddSubscription(ctx, "https://example.com/sub"); return err },
	} {
		if err := fn(); !errors.Is(err, context.Canceled) {
			t.Fatalf("want context.Canceled, got %v", err)
		}
	}
}

func TestDisconnectError(t *testing.T) {
	settings, _ := domain.Settings{}.Normalize()
	app := testCore(t, &fakeEngine{closeErr: io.ErrUnexpectedEOF}, store.PersistentState{
		Settings:      settings,
		Subscriptions: []store.Subscription{{ID: "sub", Nodes: []domain.Node{{ID: "n1"}}}},
	})
	if err := app.Connect(context.Background(), "n1", "sub"); err != nil {
		t.Fatal(err)
	}
	if err := app.Disconnect(context.Background()); err == nil {
		t.Fatal("want error on disconnect")
	}
	if state, _ := app.store.Load(); app.current().Active != (domain.NodeRef{}) || state.Active != (domain.NodeRef{}) {
		t.Fatalf("Active not cleared: current=%+v disk=%+v", app.current().Active, state.Active)
	}
}

func TestNewMalformed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "configuration.yaml")
	if err := os.WriteFile(path, []byte("invalid: [yaml: broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	logger := log.New(io.Discard, "", 0)
	conn := connection.New(context.Background(), "", nil, nil, logger)
	subs := subscription.New(context.Background(), logger)
	if _, err := New(store.Disk{Dir: dir}, conn, subs); err == nil {
		t.Fatal("want error on malformed YAML configuration")
	}
}

func TestSnapshotUptime(t *testing.T) {
	settings, _ := domain.Settings{}.Normalize()
	app := testCore(t, &fakeEngine{}, store.PersistentState{
		Settings:      settings,
		Subscriptions: []store.Subscription{{ID: "sub", Nodes: []domain.Node{{ID: "n1"}}}},
	})
	synctest.Test(t, func(t *testing.T) {
		if err := app.Connect(context.Background(), "n1", "sub"); err != nil {
			t.Fatal(err)
		}
		before := app.Snapshot()
		if !before.Status.Connected || before.Status.Uptime() != 0 {
			t.Fatalf("expected a newly connected session: %+v", before.Status)
		}
		synctest.Sleep(time.Second)
		after := app.Snapshot()
		if got := after.Status.Uptime(); got != time.Second {
			t.Fatalf("uptime = %s, want 1s", got)
		}
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("uptime changed the snapshot: before=%+v after=%+v", before, after)
		}
	})
}
