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
	for name, proxyErr := range map[string]error{"success": nil, "failure": errors.New("proxy port unavailable")} {
		t.Run(name, func(t *testing.T) {
			var logs bytes.Buffer
			eng := &fakeEngine{tunErr: permission, startErr: proxyErr}
			s := New(context.Background(), t.TempDir(), func(context.Context, string) engine.Engine { return eng }, nil, log.New(&logs, "", 0))
			settings, _ := (domain.Settings{}).Normalize()
			ref := domain.NodeRef{NodeID: "n"}
			s.Restore(domain.Node{ID: "n"}, ref, settings, true)
			st := s.Status()
			if st.Connected != (proxyErr == nil) || st.Tun {
				t.Fatalf("fallback status=%+v, proxy error=%v", st, proxyErr)
			}
			select {
			case <-s.RestartRequested():
				t.Fatal("Restore requested elevation")
			default:
			}
			if proxyErr != nil {
				if !strings.Contains(logs.String(), proxyErr.Error()) {
					t.Fatalf("proxy failure was not logged: %s", logs.String())
				}
				return
			}
			if err := s.Apply(context.Background(), domain.Node{ID: "n"}, ref, settings, true); !errors.Is(err, ipc.ErrElevate) {
				t.Fatalf("explicit TUN request: %v", err)
			}
			if s.Status() != st {
				t.Fatalf("failed TUN change replaced the working proxy status: %+v", s.Status())
			}
			select {
			case <-s.RestartRequested():
			default:
				t.Fatal("explicit TUN request did not request elevation")
			}
		})
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
