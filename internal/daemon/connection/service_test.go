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
	stopped                    bool
}

func (e *fakeEngine) Apply(_ context.Context, spec engine.SessionSpec) error {
	if spec.Tun {
		return e.tunErr
	}
	return e.startErr
}
func (e *fakeEngine) Stop() error {
	e.closeCalls++
	e.stopped = true
	return e.closeErr
}
func (e *fakeEngine) Running() bool { return !e.stopped && e.startErr == nil }

func testService(t *testing.T, eng engine.Engine) *Service {
	t.Helper()
	return &Service{
		ctx:       context.Background(),
		log:       log.New(io.Discard, "", 0),
		session:   session{eng: eng},
		newEngine: func(context.Context, string) engine.Engine { return eng },
	}
}

func TestStopCleansUpSessionOnEngineError(t *testing.T) {
	eng := &fakeEngine{closeErr: errors.New("close failed")}
	s := testService(t, eng)
	if err := s.stop(); err == nil || s.session.eng != nil || s.Status().Connected {
		t.Fatalf("stop err=%v session=%v status=%+v", err, s.session.eng, s.Status())
	}
}

func TestSetTunFailureDoesNotChangeRuntimeState(t *testing.T) {
	s := testService(t, &fakeEngine{tunErr: errors.New("tun failed")})
	settings, _ := domain.Settings{}.Normalize()
	if err := s.Apply(context.Background(), domain.Node{ID: "n1"}, domain.NodeRef{NodeID: "n1"}, settings, true); err == nil || s.session.tun {
		t.Fatalf("Apply err=%v tun=%v", err, s.session.tun)
	}
}

func TestStartClosesEngineOnFailure(t *testing.T) {
	eng := &fakeEngine{startErr: errors.New("start failed")}
	s := testService(t, nil)
	s.newEngine = func(context.Context, string) engine.Engine { return eng }
	settings, _ := domain.Settings{}.Normalize()
	if err := s.apply(context.Background(), domain.Node{ID: "n1"}, domain.NodeRef{NodeID: "n1"}, settings, false, true); err == nil || eng.closeCalls != 1 {
		t.Fatalf("start err=%v closeCalls=%d", err, eng.closeCalls)
	}
}

func TestStatusPortMatchesActiveSession(t *testing.T) {
	eng := &fakeEngine{}
	s := testService(t, nil)
	s.newEngine = func(context.Context, string) engine.Engine { return eng }
	settings, _ := domain.Settings{}.Normalize()
	settings.Port = 1085
	if err := s.Connect(context.Background(), domain.Node{ID: "n1"}, domain.NodeRef{NodeID: "n1"}, settings, false); err != nil {
		t.Fatal(err)
	}
	if st := s.Status(); !st.Connected || st.Port != 1085 {
		t.Fatalf("Connect status=%+v", st)
	}
	if err := s.Disconnect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if st := s.Status(); st.Connected || st.Port != 0 {
		t.Fatalf("Disconnect status=%+v", st)
	}
}

func TestShutdownClosesEngine(t *testing.T) {
	eng := &fakeEngine{}
	s := testService(t, eng)
	s.Shutdown()
	if eng.closeCalls != 1 {
		t.Fatalf("Shutdown calls=%d", eng.closeCalls)
	}
}
