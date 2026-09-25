package core

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/luynrs/justray/internal/daemon/connection"
	"github.com/luynrs/justray/internal/daemon/store"
	"github.com/luynrs/justray/internal/daemon/subscription"
	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/engine"
)

type fakeEngine struct {
	closeErr error
	stopped  bool
	spec     engine.SessionSpec
}

func (fake *fakeEngine) Apply(_ context.Context, spec engine.SessionSpec) error {
	fake.spec = spec
	return nil
}
func (fake *fakeEngine) Stop() error   { fake.stopped = true; return fake.closeErr }
func (fake *fakeEngine) Running() bool { return !fake.stopped }

func testCore(t *testing.T, instance engine.Engine, state store.PersistentState) *Core {
	t.Helper()
	disk := store.Disk{Dir: t.TempDir()}
	if err := disk.Save(state); err != nil {
		t.Fatal(err)
	}
	logger := log.New(io.Discard, "", 0)
	app, err := New(disk, connection.New(t.Context(), "", func(context.Context, string) engine.Engine { return instance }, nil, logger), subscription.New(t.Context(), logger))
	if err != nil {
		t.Fatal(err)
	}
	return app
}

func TestConnection(t *testing.T) {
	settings, _ := (domain.Settings{}).Normalize()
	settings.Port = 1080
	engine := &fakeEngine{}
	app := testCore(t, engine, store.PersistentState{
		Settings:      settings,
		Subscriptions: []store.Subscription{{ID: "sub", Nodes: []domain.Node{{ID: "node"}}}},
	})
	if err := app.Connect(t.Context(), "node", "sub"); err != nil {
		t.Fatal(err)
	}
	if status := app.Snapshot().Status; !status.Connected || status.Port != 1080 {
		t.Fatalf("connected status: %+v", status)
	}
	settings = app.Snapshot().Settings
	settings.DNS = "1.1.1.1"
	if err := app.SetSettings(t.Context(), settings); err != nil {
		t.Fatal(err)
	}
	if engine.spec.Settings.DNS != settings.DNS || app.Snapshot().Settings.DNS != settings.DNS {
		t.Fatalf("DNS setting did not reach engine and status: %q, %q", engine.spec.Settings.DNS, app.Snapshot().Settings.DNS)
	}
	invalid := settings
	invalid.DNS = "dns-cloudflare/dns-query"
	if err := app.SetSettings(t.Context(), invalid); err == nil || app.Snapshot().Settings.DNS != settings.DNS || engine.spec.Settings.DNS != settings.DNS {
		t.Fatalf("invalid DNS changed active settings: %v", err)
	}
	if err := app.SetTun(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	state, err := app.store.Load()
	if err != nil || !state.Tun || state.Active.NodeID != "node" || state.Settings.DNS != settings.DNS || !engine.spec.Tun {
		t.Fatalf("saved state: %+v, %v", state, err)
	}
	if err := app.Disconnect(t.Context()); err != nil {
		t.Fatal(err)
	}
	state, err = app.store.Load()
	if err != nil || state.Active != (domain.NodeRef{}) || app.Snapshot().Status.Connected {
		t.Fatalf("disconnected state: %+v, %v", state, err)
	}
}

func TestRestore(t *testing.T) {
	engine := &fakeEngine{}
	app := testCore(t, engine, store.PersistentState{
		Active:        domain.NodeRef{SubscriptionID: "sub", NodeID: "node"},
		Subscriptions: []store.Subscription{{ID: "sub", Nodes: []domain.Node{{ID: "node"}}}},
	})
	app.Restore()
	if !app.Snapshot().Status.Connected || engine.spec.Node.ID != "node" {
		t.Fatalf("saved connection was not restored: %+v", app.Snapshot().Status)
	}
	app.Shutdown()
	state, err := app.store.Load()
	if err != nil || app.Snapshot().Status.Connected || !engine.stopped || state.Active.NodeID != "node" {
		t.Fatalf("shutdown lost saved connection: %+v, %v", state, err)
	}
}

func TestDisconnectError(t *testing.T) {
	app := testCore(t, &fakeEngine{closeErr: io.ErrUnexpectedEOF}, store.PersistentState{
		Subscriptions: []store.Subscription{{ID: "sub", Nodes: []domain.Node{{ID: "node"}}}},
	})
	if err := app.Connect(t.Context(), "node", "sub"); err != nil {
		t.Fatal(err)
	}
	if err := app.Disconnect(t.Context()); err == nil {
		t.Fatal("expected disconnect error")
	}
	state, err := app.store.Load()
	if err != nil || state.Active != (domain.NodeRef{}) {
		t.Fatalf("active node remained after disconnect error: %+v, %v", state.Active, err)
	}
}

func TestProbeResults(t *testing.T) {
	app := testCore(t, &fakeEngine{}, store.PersistentState{Subscriptions: []store.Subscription{
		{ID: "link", URL: "vless://node@example.com:443", Nodes: []domain.Node{{ID: "first"}}},
		{ID: "sub", URL: "https://example.com/sub", Nodes: []domain.Node{{ID: "second"}}},
	}})
	probe := func(_ context.Context, nodes []domain.Node, _ domain.Settings, _ string, onResult func(string, engine.Result)) error {
		for _, node := range nodes {
			onResult(node.ID, engine.Result{Alive: true, MS: 10})
		}
		return nil
	}
	app.conn = connection.New(t.Context(), t.TempDir(), nil, probe, log.New(io.Discard, "", 0))
	if err := app.Probe(t.Context(), "default", ""); err != nil {
		t.Fatal(err)
	}
	if nodes := app.Snapshot().Nodes; !nodes[0].Probed || nodes[1].Probed {
		t.Fatalf("default group probe: %+v", nodes)
	}
	if err := app.Probe(t.Context(), "", ""); err != nil {
		t.Fatal(err)
	}
	for _, node := range app.Snapshot().Nodes {
		if !node.Probed || !node.Alive || node.MS != 10 {
			t.Fatalf("probe result: %+v", node)
		}
	}
}

func TestSubscriptions(t *testing.T) {
	engine := &fakeEngine{}
	app := testCore(t, engine, store.PersistentState{Subscriptions: []store.Subscription{
		{ID: "first", Nodes: []domain.Node{{ID: "node"}}},
		{ID: "second"},
	}})
	if err := app.Connect(t.Context(), "node", "first"); err != nil {
		t.Fatal(err)
	}
	_, updates, cancel := app.Watch()
	defer cancel()
	if err := app.MoveSubscription("first", 1); err != nil {
		t.Fatal(err)
	}
	if snapshot := <-updates; len(snapshot.Subscriptions) != 2 || snapshot.Subscriptions[0].ID != "second" {
		t.Fatalf("subscription order: %+v", snapshot.Subscriptions)
	}
	if err := app.RemoveSubscription("first"); err != nil {
		t.Fatal(err)
	}
	if snapshot := <-updates; snapshot.Status.Connected || snapshot.Selected.NodeID != "" || !engine.stopped {
		t.Fatalf("removed active subscription remained connected: %+v", snapshot)
	}
	added, err := app.AddSubscription(t.Context(), "vless://11111111-1111-1111-1111-111111111111@example.com:443?security=tls#new")
	if err != nil {
		t.Fatal(err)
	}
	state, err := app.store.Load()
	if err != nil || len(state.Subscriptions) != 2 || state.Subscriptions[0].ID != "second" || state.Subscriptions[1].ID != added.ID ||
		len(state.Subscriptions[1].Nodes) != 1 || state.Active.NodeID != "" {
		t.Fatalf("saved subscriptions: %+v, %v", state, err)
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

func TestNewMalformed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("invalid: {json: broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	logger := log.New(io.Discard, "", 0)
	conn := connection.New(context.Background(), "", nil, nil, logger)
	subs := subscription.New(context.Background(), logger)
	if _, err := New(store.Disk{Dir: dir}, conn, subs); err == nil {
		t.Fatal("want error on malformed JSON configuration")
	}
}
