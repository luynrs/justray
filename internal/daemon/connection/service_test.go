package connection

import (
	"context"
	"errors"
	"io"
	"log"
	"testing"

	"github.com/luynrs/justray/internal/daemon/engine"
	"github.com/luynrs/justray/internal/shared/domain"
)

type fakeEngine struct {
	startErr, tunErr, closeErr error
	closeCalls                 int
}

func (e *fakeEngine) Apply(_ context.Context, spec engine.SessionSpec) error {
	if spec.Tun {
		return e.tunErr
	}
	return e.startErr
}
func (e *fakeEngine) Stop(context.Context) error { e.closeCalls++; return e.closeErr }

func testService(t *testing.T, eng engine.Engine) *Service {
	t.Helper()
	return &Service{
		ctx:     context.Background(),
		log:     log.New(io.Discard, "", 0),
		session: session{eng: eng},
	}
}

func TestStopRetainsEngineAfterCloseFailure(t *testing.T) {
	eng := &fakeEngine{closeErr: errors.New("close failed")}
	s := testService(t, eng)
	if err := s.stop(context.Background()); err == nil || s.session.eng != eng {
		t.Fatalf("stop: err=%v engine=%v", err, s.session.eng)
	}
}

func TestStopClosesActiveAfterCleanupFailure(t *testing.T) {
	cleanup := &fakeEngine{closeErr: errors.New("cleanup failed")}
	active := &fakeEngine{}
	s := testService(t, active)
	s.cleanup = cleanup
	if err := s.stop(context.Background()); err == nil {
		t.Fatal("stop succeeded")
	}
	if active.closeCalls != 1 || s.session.eng != nil {
		t.Fatalf("active engine was not closed: calls=%d session=%v", active.closeCalls, s.session.eng)
	}
}

func TestSetTunFailureDoesNotChangeRuntimeState(t *testing.T) {
	s := testService(t, &fakeEngine{tunErr: errors.New("tun failed")})
	settings, _ := domain.Settings{}.Normalize()
	if _, err := s.Apply(context.Background(), domain.Node{ID: "n1"}, domain.NodeRef{NodeID: "n1"}, settings, true); err == nil {
		t.Fatal("Apply succeeded")
	}
	if s.session.tun {
		t.Fatal("runtime TUN changed after failure")
	}
}

func TestStartClosesEngineOnFailure(t *testing.T) {
	eng := &fakeEngine{startErr: errors.New("start failed")}
	s := testService(t, nil)
	s.newEngine = func(string) engine.Engine { return eng }
	settings, _ := domain.Settings{}.Normalize()
	if err := s.apply(domain.Node{ID: "n1"}, domain.NodeRef{NodeID: "n1"}, settings, false, true); err == nil || eng.closeCalls != 1 {
		t.Fatalf("start err=%v closeCalls=%d", err, eng.closeCalls)
	}
}

func TestShutdownClosesEngine(t *testing.T) {
	eng := &fakeEngine{}
	s := testService(t, eng)
	s.Shutdown()
	if eng.closeCalls != 1 {
		t.Fatalf("Shutdown did not close engine: calls=%d", eng.closeCalls)
	}
}
