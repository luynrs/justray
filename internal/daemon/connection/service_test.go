package connection

import (
	"context"
	"errors"
	"io"
	"log"
	"testing"

	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/engine"
)

type fakeEngine struct {
	startErr, tunErr, closeErr error
	closeCalls                 int
	stopped                    bool
	applying, stopping         func()
}

func (e *fakeEngine) Apply(_ context.Context, spec engine.SessionSpec) error {
	if e.applying != nil {
		e.applying()
	}
	err := e.startErr
	if spec.Tun {
		err = e.tunErr
	}
	if err == nil {
		e.stopped = false
	}
	return err
}
func (e *fakeEngine) Stop() error {
	if e.stopping != nil {
		e.stopping()
	}
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
		eng:       eng,
		newEngine: func(context.Context, string) engine.Engine { return eng },
	}
}

func TestStopError(t *testing.T) {
	eng := &fakeEngine{closeErr: errors.New("close failed")}
	s := testService(t, eng)
	if err := s.stop(); err == nil || s.eng != nil || s.Status().Connected {
		t.Fatalf("stop err=%v engine=%v status=%+v", err, s.eng, s.Status())
	}
}

func TestSetTunFailure(t *testing.T) {
	s := testService(t, &fakeEngine{tunErr: errors.New("tun failed")})
	settings, _ := domain.Settings{}.Normalize()
	if err := s.Apply(context.Background(), domain.Node{ID: "n1"}, domain.NodeRef{NodeID: "n1"}, settings, true); err == nil || s.Status().Tun {
		t.Fatalf("Apply err=%v status=%+v", err, s.Status())
	}
}

func TestStartFailure(t *testing.T) {
	eng := &fakeEngine{startErr: errors.New("start failed")}
	s := testService(t, nil)
	s.newEngine = func(context.Context, string) engine.Engine { return eng }
	settings, _ := domain.Settings{}.Normalize()
	if err := s.apply(context.Background(), domain.Node{ID: "n1"}, domain.NodeRef{NodeID: "n1"}, settings, false, true); err == nil || eng.closeCalls != 1 {
		t.Fatalf("start err=%v closeCalls=%d", err, eng.closeCalls)
	}
}

func TestStatusPort(t *testing.T) {
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

func TestShutdown(t *testing.T) {
	eng := &fakeEngine{}
	s := testService(t, eng)
	s.Shutdown()
	if eng.closeCalls != 1 {
		t.Fatalf("Shutdown calls=%d", eng.closeCalls)
	}
}
