package core

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luynrs/justray/internal/daemon/store"
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
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/fast" {
			_, _ = io.WriteString(response, "vless://11111111-1111-1111-1111-111111111111@example.com:443?security=tls#fast")
			return
		}
		close(started)
		<-request.Context().Done()
	}))
	defer server.Close()
	app := testCore(t, &fakeEngine{}, store.PersistentState{Subscriptions: []store.Subscription{
		{ID: "fast", URL: server.URL + "/fast"}, {ID: "slow", URL: server.URL + "/slow"},
	}})
	_, updates, stop := app.Watch()
	defer stop()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- app.RefreshSubscriptions(ctx) }()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("refresh did not start")
	}
	for app.Snapshot().Subscriptions[0].UpdatedAt.IsZero() {
		select {
		case <-updates:
		case <-ctx.Done():
			t.Fatal("fast subscription did not finish")
		}
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
	if state, err := app.store.Load(); err != nil || state.Subscriptions[0].UpdatedAt.IsZero() ||
		!state.Subscriptions[1].UpdatedAt.IsZero() || app.Snapshot().Subscriptions[1].Refreshing {
		t.Fatalf("cancellation lost completed work or saved unfinished work: %+v, %v", state.Subscriptions, err)
	}
}

func TestRefreshOrdering(t *testing.T) {
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := "B"
		if r.URL.Path == "/a" {
			name = "A2"
			if calls.Add(1) == 1 {
				name = "A1"
			}
		} else {
			close(started)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		_, _ = io.WriteString(w, "vless://11111111-1111-1111-1111-111111111111@example.com:443?security=tls#"+name)
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	app := testCore(t, &fakeEngine{}, store.PersistentState{Subscriptions: []store.Subscription{
		{ID: "a", URL: srv.URL + "/a"}, {ID: "b", URL: srv.URL + "/b"},
	}})
	_, updates, stop := app.Watch()
	defer stop()
	done := make(chan error, 1)
	go func() { done <- app.RefreshSubscriptions(ctx) }()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("refresh did not start")
	}
	mutated := make(chan error, 1)
	go func() { mutated <- app.SetCollapsed("b", true) }()
	select {
	case err := <-mutated:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("mutation blocked on refresh I/O")
	}
	for app.Snapshot().Subscriptions[0].UpdatedAt.IsZero() || app.Snapshot().Subscriptions[0].Refreshing {
		select {
		case <-updates:
		case <-ctx.Done():
			t.Fatal("a waited for b before finishing")
		}
	}
	if !app.Snapshot().Subscriptions[1].Refreshing {
		t.Fatal("b finished early")
	}
	if err := app.RefreshSubscriptions(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("refresh did not finish")
	}
	state, err := app.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Subscriptions[0].Nodes) == 0 || len(state.Subscriptions[1].Nodes) == 0 || state.Subscriptions[0].Nodes[0].Name != "A2" || state.Subscriptions[1].Nodes[0].Name != "B" || calls.Load() != 2 {
		t.Fatalf("stale refresh overwrote newer result: %+v", state.Subscriptions)
	}
	if len(state.Collapsed) != 1 || state.Collapsed[0] != "b" || app.Snapshot().Subscriptions[0].Refreshing || app.Snapshot().Subscriptions[1].Refreshing {
		t.Fatal("refresh lost concurrent mutation or remained active")
	}
}
