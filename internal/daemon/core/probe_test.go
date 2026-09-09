package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"testing"
	"time"

	"github.com/luynrs/justray/internal/daemon/connection"
	"github.com/luynrs/justray/internal/daemon/store"
	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/engine"
)

func probeCore(t testing.TB, n int, probe engine.ProbeFunc) *Core {
	t.Helper()
	nodes := make([]domain.Node, n)
	for i := range nodes {
		nodes[i] = domain.Node{ID: fmt.Sprint(i), Name: "example node", Server: "127.0.0.1", Port: 443}
	}
	app := testCore(t, &fakeEngine{}, store.PersistentState{Subscriptions: []store.Subscription{{ID: "s", Nodes: nodes}}})
	app.conn = connection.New(context.Background(), t.TempDir(), nil, probe, log.New(io.Discard, "", 0))
	return app
}

func instantProbe(_ context.Context, nodes []domain.Node, _ domain.Settings, _ string, onResult func(string, engine.Result)) error {
	for _, node := range nodes {
		onResult(node.ID, engine.Result{Alive: true, MS: 10})
	}
	return nil
}

func TestProbeBatchesResults(t *testing.T) {
	const n = 512
	app := probeCore(t, n, instantProbe)
	before := app.Snapshot().Revision
	if err := app.Probe(context.Background(), "s", ""); err != nil {
		t.Fatal(err)
	}
	after := app.Snapshot()
	if after.Revision-before >= n/4 {
		t.Fatalf("probe rebuilt %d snapshots for a burst of %d results", after.Revision-before, n)
	}
	for _, node := range after.Nodes {
		if node.Probing || !node.Probed || !node.Alive || node.MS != 10 {
			t.Fatalf("incomplete final result: %+v", node)
		}
	}
}

func TestProbeAlreadyCancelled(t *testing.T) {
	app := probeCore(t, 2, instantProbe)
	before := app.Snapshot().Revision
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := app.Probe(ctx, "s", ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled probe: %v", err)
	}
	if app.Snapshot().Revision != before {
		t.Fatal("cancelled probe changed published state")
	}
}

func TestProbePublishesBeforeCompletionAndFlushesOnCancel(t *testing.T) {
	probe := func(ctx context.Context, nodes []domain.Node, _ domain.Settings, _ string, onResult func(string, engine.Result)) error {
		onResult(nodes[0].ID, engine.Result{Alive: true, MS: 10})
		<-ctx.Done()
		return ctx.Err()
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

func BenchmarkProbe(b *testing.B) {
	for _, n := range []int{128, 512} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			app := probeCore(b, n, instantProbe)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if err := app.Probe(context.Background(), "s", ""); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
