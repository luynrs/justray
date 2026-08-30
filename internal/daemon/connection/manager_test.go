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
	startErr, tunErr, closeErr error
	closeCalls                 int
}

func (e *fakeEngine) Start(domain.Node, bool) error { return e.startErr }
func (*fakeEngine) Swap(domain.Node) error          { return nil }
func (e *fakeEngine) TunAdd() error                 { return e.tunErr }
func (*fakeEngine) TunRemove() error                { return nil }
func (e *fakeEngine) Close() error                  { e.closeCalls++; return e.closeErr }

func testService(t *testing.T, eng engine.Engine) *Service {
	t.Helper()
	settings, _ := domain.Settings{}.Normalize()
	return &Service{
		store: store.Disk{Dir: t.TempDir()}, log: log.New(io.Discard, "", 0),
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

func TestStartClosesEngineOnFailure(t *testing.T) {
	eng := &fakeEngine{startErr: errors.New("start failed")}
	s := testService(t, nil)
	s.newEngine = func(domain.Settings, string) engine.Engine { return eng }
	if err := s.start(domain.Node{ID: "n1"}, domain.NodeRef{NodeID: "n1"}); err == nil || eng.closeCalls != 1 {
		t.Fatalf("start err=%v closeCalls=%d", err, eng.closeCalls)
	}
}

func TestFind(t *testing.T) {
	subs := []store.Subscription{
		{ID: "a", Nodes: []domain.Node{{ID: "0123456789abcdef"}}},
		{ID: "b", Nodes: []domain.Node{{ID: "0123fedcba987654"}}},
	}
	if _, _, err := find(subs, domain.NodeRef{NodeID: "ffff"}); err == nil || err.Error() != `node "ffff" not found` {
		t.Fatalf("missing node: %v", err)
	}
	if _, _, err := find(subs, domain.NodeRef{NodeID: "0123"}); err == nil || err.Error() != `ambiguous node ID "0123"` {
		t.Fatalf("ambiguous node: %v", err)
	}
	_, ref, err := find(subs, domain.NodeRef{NodeID: "01234567"})
	if err != nil || ref != (domain.NodeRef{SubscriptionID: "a", NodeID: "0123456789abcdef"}) {
		t.Fatalf("unique node: ref=%+v err=%v", ref, err)
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
