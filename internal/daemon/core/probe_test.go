package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"sync"
	"testing"
	"time"

	"github.com/luynrs/justray/internal/daemon/connection"
	"github.com/luynrs/justray/internal/daemon/store"
	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/engine"
	"github.com/luynrs/justray/internal/ipc"
)

func probeCore(t *testing.T, n int, probe func(context.Context, domain.Node, domain.Settings, string) (engine.Result, error)) *Core {
	t.Helper()
	nodes := make([]domain.Node, n)
	for i := range nodes {
		nodes[i] = domain.Node{ID: fmt.Sprint(i), Name: "example node", Server: fmt.Sprintf("node-%d.example", i), Port: 443}
	}
	app := testCore(t, &fakeEngine{}, store.State{Subscriptions: []store.Subscription{{ID: "s", Nodes: nodes}}})
	app.conn = connection.New(context.Background(), t.TempDir(), nil, func(targets []engine.Target, settings domain.Settings, logPath string, onResult func(int, engine.Result, error)) {
		var workers sync.WaitGroup
		for i, target := range targets {
			workers.Go(func() {
				result, err := probe(target.Context, target.Node, settings, logPath)
				onResult(i, result, err)
			})
		}
		workers.Wait()
	}, log.New(io.Discard, "", 0))
	return app
}

func instantProbe(_ context.Context, _ domain.Node, _ domain.Settings, _ string) (engine.Result, error) {
	return engine.Result{Alive: true, Duration: 10}, nil
}

func TestProbeBatch(t *testing.T) {
	const n = 512
	var app *Core
	var updates int
	var observed sync.Mutex
	var previous *ipc.Snapshot
	probe := func(_ context.Context, _ domain.Node, _ domain.Settings, _ string) (engine.Result, error) {
		observed.Lock()
		defer observed.Unlock()
		if current := app.snapshot.Load(); current != previous {
			updates++
			previous = current
		}
		return engine.Result{Alive: true, Duration: 10}, nil
	}
	app = probeCore(t, n, probe)
	if err := app.Probe(context.Background(), "s", ""); err != nil {
		t.Fatal(err)
	}
	after := app.Snapshot()
	if updates >= n/4 {
		t.Fatalf("probe published %d intermediate snapshots for a burst of %d results", updates, n)
	}
	for _, node := range after.Nodes {
		if node.Probing || !node.Probed || !node.Alive || node.Duration != 10 {
			t.Fatalf("incomplete final result: %+v", node)
		}
	}
}

func TestProbeCanceled(t *testing.T) {
	app := probeCore(t, 2, instantProbe)
	before := app.snapshot.Load()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := app.Probe(ctx, "s", ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled probe: %v", err)
	}
	if app.snapshot.Load() != before {
		t.Fatal("cancelled probe changed published state")
	}
}

func TestProbeProgress(t *testing.T) {
	probe := func(ctx context.Context, node domain.Node, _ domain.Settings, _ string) (engine.Result, error) {
		if node.ID == "0" {
			return engine.Result{Alive: true, Duration: 10}, nil
		}
		<-ctx.Done()
		return engine.Result{}, ctx.Err()
	}
	app := probeCore(t, 2, probe)
	_, updates, stop := app.Watch()
	defer stop()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- app.Probe(ctx, "s", "") }()
	deadline := time.After(time.Second)
	for {
		select {
		case update := <-updates:
			if update.Nodes[0].Probed && update.Nodes[1].Probing {
				cancel()
				if err := <-done; !errors.Is(err, context.Canceled) {
					t.Fatalf("cancelled probe: %v", err)
				}
				final := app.Snapshot()
				if !final.Nodes[0].Probed || final.Nodes[0].Probing || final.Nodes[1].Probing {
					t.Fatalf("cancellation left incomplete results: %+v", final.Nodes)
				}
				return
			}
		case <-deadline:
			cancel()
			<-done
			t.Fatal("results were withheld until the slow probe completed")
		}
	}
}
