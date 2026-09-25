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

func (fake *fakeEngine) Apply(_ context.Context, spec engine.SessionSpec) error {
	if fake.applying != nil {
		fake.applying()
	}
	err := fake.startErr
	if spec.Tun {
		err = fake.tunErr
	}
	if err == nil {
		fake.stopped = false
	}
	return err
}
func (fake *fakeEngine) Stop() error {
	if fake.stopping != nil {
		fake.stopping()
	}
	fake.closeCalls++
	fake.stopped = true
	return fake.closeErr
}
func (fake *fakeEngine) Running() bool { return !fake.stopped && fake.startErr == nil }

func testService(t *testing.T, instance engine.Engine) *Service {
	t.Helper()
	return &Service{
		ctx:       context.Background(),
		log:       log.New(io.Discard, "", 0),
		eng:       instance,
		newEngine: func(context.Context, string) engine.Engine { return instance },
	}
}

func TestStartFailure(t *testing.T) {
	fake := &fakeEngine{startErr: errors.New("start failed")}
	service := testService(t, nil)
	service.newEngine = func(context.Context, string) engine.Engine { return fake }
	settings, _ := domain.Settings{}.Normalize()
	if err := service.apply(t.Context(), domain.Node{ID: "node"}, domain.NodeRef{NodeID: "node"}, settings, false, true); err == nil || fake.closeCalls != 1 {
		t.Fatalf("start err=%v closeCalls=%d", err, fake.closeCalls)
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

func TestShutdown(t *testing.T) {
	eng := &fakeEngine{}
	s := testService(t, eng)
	s.Shutdown()
	if eng.closeCalls != 1 {
		t.Fatalf("Shutdown calls=%d", eng.closeCalls)
	}
}
