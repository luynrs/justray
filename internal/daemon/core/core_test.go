package core

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luynrs/justray/internal/daemon/connection"
	"github.com/luynrs/justray/internal/daemon/engine"
	"github.com/luynrs/justray/internal/daemon/store"
	"github.com/luynrs/justray/internal/daemon/subscription"
	"github.com/luynrs/justray/internal/shared/domain"
)

func TestRefreshRunsOutsideMutationLockAndJoins(t *testing.T) {
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
	app := New(disk, connection.New(context.Background(), "", nil, nil, logger), subs, logger)
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

func TestMoveSubscriptionCommitsCoreState(t *testing.T) {
	disk := store.Disk{Dir: t.TempDir()}
	if err := disk.Save(store.PersistentState{Subscriptions: []store.Subscription{{ID: "a"}, {ID: "b"}}}); err != nil {
		t.Fatal(err)
	}
	logger := log.New(io.Discard, "", 0)
	app := New(disk, connection.New(context.Background(), "", nil, nil, logger), subscription.New(context.Background(), logger), logger)
	initial, changed, cancel := app.Watch()
	defer cancel()
	if err := app.MoveSubscription("a", 1); err != nil {
		t.Fatal(err)
	}
	select {
	case update := <-changed:
		if update.Revision <= initial.Revision || app.Snapshot().Revision != update.Revision {
			t.Fatalf("revision: initial=%d update=%d snapshot=%d", initial.Revision, update.Revision, app.Snapshot().Revision)
		}
	case <-time.After(time.Second):
		t.Fatal("watch did not receive mutation revision")
	}
	state, err := disk.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := state.Subscriptions; len(got) != 2 || got[0].ID != "b" || got[1].ID != "a" {
		t.Fatalf("subscriptions = %+v", got)
	}
}

func TestRefreshSubscriptionPublishesCommittedSnapshot(t *testing.T) {
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
	app := New(disk, connection.New(context.Background(), "", nil, nil, logger), subscription.New(context.Background(), logger), logger)
	initial, changed, cancel := app.Watch()
	defer cancel()
	if _, err := app.RefreshSubscription(context.Background(), "sub"); err != nil {
		t.Fatal(err)
	}
	select {
	case update := <-changed:
		snapshot := app.Snapshot()
		if update.Revision <= initial.Revision || snapshot.Revision != update.Revision || len(snapshot.Nodes) != 1 {
			t.Fatalf("update=%+v initial=%+v snapshot=%+v", update, initial, snapshot)
		}
	case <-time.After(time.Second):
		t.Fatal("watch did not receive refresh revision")
	}
}

func TestProbePublishesCoreOwnedResults(t *testing.T) {
	disk := store.Disk{Dir: t.TempDir()}
	if err := disk.Save(store.PersistentState{Subscriptions: []store.Subscription{{ID: "sub", Nodes: []domain.Node{{ID: "node"}}}}}); err != nil {
		t.Fatal(err)
	}
	logger := log.New(io.Discard, "", 0)
	probe := func(context.Context, []domain.Node, domain.Settings, string) (map[string]engine.Result, error) {
		return map[string]engine.Result{"node": {Alive: true, MS: 12}}, nil
	}
	app := New(disk, connection.New(context.Background(), "", nil, probe, logger), subscription.New(context.Background(), logger), logger)
	initial, changed, cancel := app.Watch()
	defer cancel()
	if err := app.Probe(context.Background(), "sub", "node"); err != nil {
		t.Fatal(err)
	}
	select {
	case update := <-changed:
		snapshot := app.Snapshot()
		if update.Revision <= initial.Revision || snapshot.Revision != update.Revision || len(snapshot.Nodes) != 1 || !snapshot.Nodes[0].Probed || !snapshot.Nodes[0].Alive || snapshot.Nodes[0].MS != 12 {
			t.Fatalf("update=%+v initial=%+v snapshot=%+v", update, initial, snapshot)
		}
	case <-time.After(time.Second):
		t.Fatal("watch did not receive probe revision")
	}
}

func TestSetTunCommitsCoreState(t *testing.T) {
	disk := store.Disk{Dir: t.TempDir()}
	logger := log.New(io.Discard, "", 0)
	app := New(disk, connection.New(context.Background(), "", nil, nil, logger), subscription.New(context.Background(), logger), logger)
	initial, changed, cancel := app.Watch()
	defer cancel()
	if _, err := app.SetTun(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	select {
	case update := <-changed:
		snapshot := app.Snapshot()
		if update.Revision <= initial.Revision || snapshot.Revision != update.Revision || !snapshot.Status.Tun {
			t.Fatalf("update=%+v initial=%+v snapshot=%+v", update, initial, snapshot)
		}
	case <-time.After(time.Second):
		t.Fatal("watch did not receive TUN revision")
	}
	state, err := disk.Load()
	if err != nil || !state.Tun {
		t.Fatalf("state=%+v err=%v", state, err)
	}
}
