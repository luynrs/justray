package engine

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/luynrs/justray/internal/domain"
)

func TestProbeCanceled(t *testing.T) {
	settings, _ := (domain.Settings{}).Normalize()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := Probe(ctx, []domain.Node{{ID: "node"}}, settings, "", func(int, Result) {
		t.Error("canceled probe published a result")
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("probe error: %v", err)
	}
}

func TestProbeStartError(t *testing.T) {
	settings, _ := (domain.Settings{}).Normalize()
	settings.LogLevel = "invalid"
	err := Probe(t.Context(), []domain.Node{{ID: "node", Protocol: domain.HTTP, Server: "127.0.0.1", Port: 80}}, settings, "", func(int, Result) {
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
	err := Probe(ctx, []domain.Node{{ID: "node", Protocol: domain.HTTP, Server: "127.0.0.1", Port: server.Listener.Addr().(*net.TCPAddr).Port}}, settings, "", func(_ int, next Result) {
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
	err := Probe(t.Context(), []domain.Node{{ID: "node", Protocol: domain.HTTP, Server: "127.0.0.1", Port: server.Listener.Addr().(*net.TCPAddr).Port}}, settings, "", func(_ int, next Result) {
		result = next
	})
	if err != nil || result.Alive || result.Duration != 0 || result.Failure != "failed" || result.Error == "" {
		t.Fatalf("failed request: result=%+v, error=%v", result, err)
	}
}
