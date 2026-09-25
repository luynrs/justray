package connection

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/engine"
	"github.com/luynrs/justray/internal/ipc"
	"github.com/luynrs/justray/internal/platform/elevate"
)

func TestRestoreFallback(t *testing.T) {
	permission := errors.Join(os.ErrPermission, errors.New("operation not permitted"))
	if !elevate.Needed(permission) {
		t.Skip("requires a process without TUN privileges")
	}
	var logs bytes.Buffer
	fake := &fakeEngine{tunErr: permission}
	service := New(t.Context(), t.TempDir(), func(context.Context, string) engine.Engine { return fake }, nil, log.New(&logs, "", 0))
	settings, _ := (domain.Settings{}).Normalize()
	reference := domain.NodeRef{NodeID: "node"}
	service.Restore(domain.Node{ID: "node"}, reference, settings, true)
	status := service.Status()
	if status.Connected {
		t.Fatalf("expected disconnected when TUN needs elevation, got status=%+v", status)
	}
	if !strings.Contains(logs.String(), "tun requires elevation") {
		t.Fatalf("elevation requirement was not logged: %s", logs.String())
	}
	select {
	case <-service.RestartRequested():
		t.Fatal("Restore requested elevation")
	default:
	}
}

func TestStatusDuringEngineOperations(t *testing.T) {
	fake := &fakeEngine{}
	service := testService(t, nil)
	service.newEngine = func(context.Context, string) engine.Engine { return fake }
	settings, _ := (domain.Settings{}).Normalize()
	reference := domain.NodeRef{NodeID: "node"}

	for _, operation := range []string{"connect", "apply", "disconnect"} {
		t.Run(operation, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			block := func() { close(entered); <-release }
			previous := service.Status()
			var run func() error
			switch operation {
			case "connect":
				fake.applying = block
				run = func() error { return service.Connect(t.Context(), domain.Node{ID: "node"}, reference, settings, false) }
			case "apply":
				fake.applying = block
				settings.Port++
				run = func() error { return service.Apply(t.Context(), domain.Node{ID: "node"}, reference, settings, false) }
			case "disconnect":
				fake.stopping = block
				run = func() error { return service.Disconnect(t.Context()) }
				previous = ipc.Status{}
			}
			done := make(chan error, 1)
			go func() { done <- run() }()
			<-entered
			read := make(chan ipc.Status, 1)
			go func() { read <- service.Status() }()
			select {
			case status := <-read:
				if status != previous {
					t.Errorf("in-flight status=%+v, want %+v", status, previous)
				}
			case <-time.After(time.Second):
				t.Error("status read blocked on the engine")
			}
			close(release)
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			fake.applying, fake.stopping = nil, nil
		})
	}
}
