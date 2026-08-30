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

	"github.com/luynrs/justray/internal/daemon/store"
	"github.com/luynrs/justray/internal/daemon/subscription"
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
	subs := subscription.New(disk, log.New(io.Discard, "", 0))
	app := New(nil, subs)
	sub, err := subs.Get("sub")
	if err != nil {
		t.Fatal(err)
	}

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
