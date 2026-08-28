package connection

import (
	"errors"
	"io"
	"log"
	"testing"

	"github.com/luynrs/justray/internal/daemon/engine"
	"github.com/luynrs/justray/internal/daemon/store"
	"github.com/luynrs/justray/internal/shared/domain"
)

type fakeEngine struct {
	tunErr, closeErr error
}

func (*fakeEngine) Start(domain.Node, bool) error { return nil }
func (*fakeEngine) Swap(domain.Node) error        { return nil }
func (e *fakeEngine) TunAdd() error               { return e.tunErr }
func (*fakeEngine) TunRemove() error              { return nil }
func (e *fakeEngine) Close() error                { return e.closeErr }

func testService(t *testing.T, eng engine.Engine) *Service {
	t.Helper()
	settings, _ := domain.Settings{}.Normalize()
	return &Service{
		store: store.Disk{Dir: t.TempDir()}, log: log.New(io.Discard, "", 0),
		settings: settings, session: session{eng: eng},
		watchers: map[chan Status]struct{}{}, probes: map[string]engine.Result{},
	}
}

func TestStopRetainsEngineAfterCloseFailure(t *testing.T) {
	eng := &fakeEngine{closeErr: errors.New("close failed")}
	s := testService(t, eng)
	if err := s.stop(); err == nil || s.session.eng != eng {
		t.Fatalf("stop: err=%v engine=%v", err, s.session.eng)
	}
}

func TestSetTunFailureDoesNotCommitState(t *testing.T) {
	s := testService(t, &fakeEngine{tunErr: errors.New("tun failed")})
	if _, err := s.SetTun(true); err == nil {
		t.Fatal("SetTun succeeded")
	}
	state, err := s.store.State()
	if err != nil || s.tun || state.Tun {
		t.Fatalf("tun committed after failure: desired=%v persisted=%v err=%v", s.tun, state.Tun, err)
	}
}
