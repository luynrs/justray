package engine

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luynrs/justray/internal/domain"
)

func TestProbeCanceled(t *testing.T) {
	settings, _ := (domain.Settings{}).Normalize()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := New(t.Context(), "").Probe(ctx, []domain.Node{{ID: "node"}}, settings, func(int, Result) {
		t.Error("canceled probe published a result")
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("probe error: %v", err)
	}
}

func TestProbeStartError(t *testing.T) {
	settings, _ := (domain.Settings{}).Normalize()
	settings.LogLevel = "invalid"
	err := New(t.Context(), "").Probe(t.Context(), []domain.Node{{ID: "node", Protocol: domain.HTTP, Server: "127.0.0.1", Port: 80}}, settings, func(int, Result) {
		t.Error("failed initialization published a result")
	})
	if err == nil {
		t.Fatal("runtime initialization failure was lost")
	}
}

func TestProbeCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		cancel()
		writer.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()
	settings, _ := (domain.Settings{}).Normalize()
	settings.ProbeURL = "http://example.test/"
	var result Result
	err := New(t.Context(), "").Probe(ctx, []domain.Node{{ID: "node", Protocol: domain.HTTP, Server: "127.0.0.1", Port: server.Listener.Addr().(*net.TCPAddr).Port}}, settings, func(_ int, next Result) {
		result = next
	})
	if !errors.Is(err, context.Canceled) || result != (Result{}) {
		t.Fatalf("canceled probe: result=%+v, error=%v", result, err)
	}
}

func TestProbeFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		time.Sleep(10 * time.Millisecond)
		writer.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()
	settings, _ := (domain.Settings{}).Normalize()
	settings.ProbeURL = "http://example.test/"
	var result Result
	err := New(t.Context(), "").Probe(t.Context(), []domain.Node{{ID: "node", Protocol: domain.HTTP, Server: "127.0.0.1", Port: server.Listener.Addr().(*net.TCPAddr).Port}}, settings, func(_ int, next Result) {
		result = next
	})
	if err != nil || result.Alive || result.Duration != 0 || result.Failure != "failed" || result.Error == "" {
		t.Fatalf("failed request: result=%+v, error=%v", result, err)
	}
}

func TestProbeHTTPStatus(t *testing.T) {
	settings, _ := (domain.Settings{}).Normalize()
	instance, err := startBox(withRegistry(t.Context()), *probeConfig(settings, ""))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = instance.Close() }()
	dialer, _ := instance.Outbound().Outbound("direct")

	for _, status := range []int{http.StatusNoContent, http.StatusFound, http.StatusBadRequest, http.StatusBadGateway} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.Method != http.MethodHead {
					t.Errorf("probe method: %s", request.Method)
				}
				writer.Header().Set("Location", "/redirect")
				writer.WriteHeader(status)
			}))
			defer server.Close()
			result, err := probe(t.Context(), domain.Node{}, nil, "direct", server.URL, dialer)
			if err != nil {
				t.Fatal(err)
			}
			if status < http.StatusBadRequest {
				if !result.Alive || result.Failure != "" || result.Error != "" {
					t.Fatalf("HTTP %d: %+v", status, result)
				}
			} else if result.Alive || result.Duration != 0 || result.Failure != "failed" || result.Error == "" {
				t.Fatalf("HTTP %d: %+v", status, result)
			}
		})
	}
}

func TestProbeConcurrencyAndCancellation(t *testing.T) {
	settings, _ := (domain.Settings{}).Normalize()
	settings.ProbeURL = "http://example.test/"
	var mutex sync.Mutex
	var active, peak int
	var perServer, perServerPeak [6]int
	started := make(chan struct{}, 24)
	var nodes []domain.Node
	for serverIndex := range perServer {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			mutex.Lock()
			active++
			perServer[serverIndex]++
			peak = max(peak, active)
			perServerPeak[serverIndex] = max(perServerPeak[serverIndex], perServer[serverIndex])
			mutex.Unlock()
			defer func() {
				mutex.Lock()
				active--
				perServer[serverIndex]--
				mutex.Unlock()
			}()
			started <- struct{}{}
			<-request.Context().Done()
		}))
		t.Cleanup(server.Close)
		for range 4 {
			nodes = append(nodes, domain.Node{
				Protocol: domain.HTTP, Server: "127.0.0.1",
				Port: server.Listener.Addr().(*net.TCPAddr).Port,
			})
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	var published atomic.Int32
	finished := make(chan error, 1)
	go func() {
		finished <- New(t.Context(), "").Probe(ctx, nodes, settings, func(int, Result) { published.Add(1) })
		close(finished)
	}()
	defer func() {
		cancel()
		if err := <-finished; err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("probe cleanup: %v", err)
		}
	}()
	for range 8 {
		select {
		case <-started:
		case err := <-finished:
			t.Fatalf("probe ended before reaching the concurrency limit: %v", err)
		case <-ctx.Done():
			t.Fatal("probe did not fill the available slots")
		}
	}
	cancel()
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled batch: %v", err)
	}
	mutex.Lock()
	defer mutex.Unlock()
	if peak != 8 {
		t.Errorf("peak concurrent probes: %d, want 8", peak)
	}
	for serverIndex, count := range perServerPeak {
		if count > 2 {
			t.Errorf("server %d reached %d concurrent probes, want at most 2", serverIndex, count)
		}
	}
	if published.Load() != 0 {
		t.Errorf("canceled probes published %d completed results", published.Load())
	}
}
