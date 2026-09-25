package core

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luynrs/justray/internal/daemon/connection"
	"github.com/luynrs/justray/internal/daemon/store"
	"github.com/luynrs/justray/internal/daemon/subscription"
	"github.com/luynrs/justray/internal/domain"
)

func TestRefreshSelected(t *testing.T) {
	var fetched atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/bad":
			response.WriteHeader(http.StatusServiceUnavailable)
		case "/unselected":
			fetched.Store(true)
		default:
			_, _ = io.WriteString(response, "vless://11111111-1111-1111-1111-111111111111@example.com:443?security=tls#node")
		}
	}))
	defer server.Close()
	app := testCore(t, &fakeEngine{}, store.PersistentState{Subscriptions: []store.Subscription{
		{ID: "good", URL: server.URL + "/good", Nodes: []domain.Node{{ID: "old"}}},
		{ID: "bad", Name: "original", URL: server.URL + "/bad"},
		{ID: "unselected", URL: server.URL + "/unselected"},
	}})
	if err := app.Connect(t.Context(), "old", "good"); err != nil {
		t.Fatal(err)
	}
	refreshErr := app.RefreshSubscriptions(t.Context(), "good", "bad", "missing")
	if refreshErr == nil || !strings.Contains(refreshErr.Error(), "bad") || !strings.Contains(refreshErr.Error(), "503") || !strings.Contains(refreshErr.Error(), "missing") {
		t.Fatalf("refresh errors: %v", refreshErr)
	}
	state, err := app.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Subscriptions[0].Nodes) != 1 || state.Subscriptions[0].UpdatedAt.IsZero() {
		t.Fatalf("successful refresh was not saved: %+v", state.Subscriptions[0])
	}
	if state.Subscriptions[1].Name != "original" || !state.Subscriptions[1].UpdatedAt.IsZero() || fetched.Load() {
		t.Fatalf("failed or unselected subscription changed: %+v", state.Subscriptions)
	}
	if state.Active != (domain.NodeRef{}) || app.Snapshot().Status.Connected {
		t.Fatalf("removed node remained connected: %+v", state.Active)
	}
}

func TestRefreshCanceled(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(started)
		<-request.Context().Done()
	}))
	defer server.Close()
	app := testCore(t, &fakeEngine{}, store.PersistentState{Subscriptions: []store.Subscription{{ID: "sub", URL: server.URL}}})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- app.RefreshSubscriptions(ctx) }()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("refresh did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("refresh error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("refresh did not stop")
	}
	if app.Snapshot().Subscriptions[0].Refreshing || !app.current().Subscriptions[0].UpdatedAt.IsZero() {
		t.Fatal("canceled refresh changed state or remained active")
	}
}

func TestRefreshLock(t *testing.T) {
	var calls atomic.Int32
	var start sync.Once
	started := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		start.Do(func() { close(started) })
		<-release
		_, _ = io.WriteString(w, "vless://11111111-1111-1111-1111-111111111111@example.com:443?security=tls#node")
	}))
	defer srv.Close()

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

func TestRefreshSnapshot(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "vless://11111111-1111-1111-1111-111111111111@example.com:443?security=tls#node")
	}))
	defer srv.Close()

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
	if err := app.RefreshSubscriptions(context.Background(), "sub"); err != nil {
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
