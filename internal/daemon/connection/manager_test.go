package connection

import (
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

func (e *fakeEngine) Apply(spec engine.SessionSpec) error {
	if spec.Tun {
		return e.tunErr
	}
	return e.startErr
}
func (e *fakeEngine) Stop() error { e.closeCalls++; return e.closeErr }

func testService(t *testing.T, eng engine.Engine) *Service {
	t.Helper()
	settings, _ := domain.Settings{}.Normalize()
	return &Service{
		log:      log.New(io.Discard, "", 0),
		settings: settings, session: session{eng: eng},
		watchers: map[chan Status]struct{}{}, probes: map[domain.NodeRef]engine.Result{},
	}
}

func TestStopRetainsEngineAfterCloseFailure(t *testing.T) {
	eng := &fakeEngine{closeErr: errors.New("close failed")}
	s := testService(t, eng)
	if err := s.stop(); err == nil || s.session.eng != eng {
		t.Fatalf("stop: err=%v engine=%v", err, s.session.eng)
	}
}

func TestStopClosesActiveAfterCleanupFailure(t *testing.T) {
	cleanup := &fakeEngine{closeErr: errors.New("cleanup failed")}
	active := &fakeEngine{}
	s := testService(t, active)
	s.cleanup = cleanup
	if err := s.stop(); err == nil {
		t.Fatal("stop succeeded")
	}
	if active.closeCalls != 1 || s.session.eng != nil {
		t.Fatalf("active engine was not closed: calls=%d session=%v", active.closeCalls, s.session.eng)
	}
}

func TestSetTunFailureDoesNotChangeRuntimeState(t *testing.T) {
	s := testService(t, &fakeEngine{tunErr: errors.New("tun failed")})
	if _, err := s.SetTun(true); err == nil {
		t.Fatal("SetTun succeeded")
	}
	if s.tun {
		t.Fatal("tun changed after failure")
	}
}

func TestStartClosesEngineOnFailure(t *testing.T) {
	eng := &fakeEngine{startErr: errors.New("start failed")}
	s := testService(t, nil)
	s.newEngine = func(string) engine.Engine { return eng }
	if err := s.apply(domain.Node{ID: "n1"}, domain.NodeRef{NodeID: "n1"}, s.current(), false, true); err == nil || eng.closeCalls != 1 {
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
