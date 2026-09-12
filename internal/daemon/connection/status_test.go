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
	eng := &fakeEngine{tunErr: permission}
	s := New(context.Background(), t.TempDir(), func(context.Context, string) engine.Engine { return eng }, nil, log.New(&logs, "", 0))
	settings, _ := (domain.Settings{}).Normalize()
	ref := domain.NodeRef{NodeID: "n"}
	s.Restore(domain.Node{ID: "n"}, ref, settings, true)
	st := s.Status()
	if st.Connected {
		t.Fatalf("expected disconnected when TUN needs elevation, got status=%+v", st)
	}
	if !strings.Contains(logs.String(), "tun requires elevation") {
		t.Fatalf("elevation requirement was not logged: %s", logs.String())
	}
	select {
	case <-s.RestartRequested():
		t.Fatal("Restore requested elevation")
	default:
	}
}

func TestStatusDuringEngineOperations(t *testing.T) {
	eng := &fakeEngine{}
	s := testService(t, nil)
	s.newEngine = func(context.Context, string) engine.Engine { return eng }
	settings, _ := (domain.Settings{}).Normalize()
	ref := domain.NodeRef{NodeID: "n"}

	for _, op := range []string{"connect", "apply", "disconnect"} {
		t.Run(op, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			block := func() { close(entered); <-release }
			previous := s.Status()
			var run func() error
			switch op {
			case "connect":
				eng.applying = block
				run = func() error { return s.Connect(context.Background(), domain.Node{ID: "n"}, ref, settings, false) }
			case "apply":
				eng.applying = block
				settings.Port++
				run = func() error { return s.Apply(context.Background(), domain.Node{ID: "n"}, ref, settings, false) }
			case "disconnect":
				eng.stopping = block
				run = func() error { return s.Disconnect(context.Background()) }
				previous = ipc.Status{}
			}
			done := make(chan error, 1)
			go func() { done <- run() }()
			<-entered
			read := make(chan ipc.Status, 1)
			go func() { read <- s.Status() }()
			select {
			case st := <-read:
				if st != previous {
					t.Errorf("in-flight status=%+v, want %+v", st, previous)
				}
			case <-time.After(time.Second):
				t.Error("status read blocked on the engine")
			}
			close(release)
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			eng.applying, eng.stopping = nil, nil
		})
	}
}
