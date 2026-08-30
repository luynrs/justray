package connection

import (
	"log"
	"sync"
	"time"

	"github.com/luynrs/justray/internal/daemon/engine"
	"github.com/luynrs/justray/internal/daemon/platform/elevate"
	"github.com/luynrs/justray/internal/shared/domain"
	"github.com/luynrs/justray/internal/shared/rpc"
)

type Status = rpc.Status

type Service struct {
	newEngine engine.New
	probeAll  engine.Probe
	log       *log.Logger
	dir       string

	opMu sync.Mutex

	mu       sync.Mutex
	session  session
	cleanup  engine.Engine
	tun      bool
	settings domain.Settings
	probes   map[domain.NodeRef]engine.Result

	watchers map[chan Status]struct{}
	restart  chan struct{}
}

func New(dir string, newEngine engine.New, probe engine.Probe, logger *log.Logger) *Service {
	return &Service{
		newEngine: newEngine,
		probeAll:  probe,
		log:       logger,
		dir:       dir,
		probes:    map[domain.NodeRef]engine.Result{},
		watchers:  map[chan Status]struct{}{},
		restart:   make(chan struct{}, 1),
	}
}

func (s *Service) Configure(settings domain.Settings, tun bool) {
	s.mu.Lock()
	s.settings, s.tun = settings, tun
	s.mu.Unlock()
}

func (s *Service) current() domain.Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settings
}

func (s *Service) ApplySettings(in domain.Settings) (Status, error) {
	s.opMu.Lock()
	defer s.opMu.Unlock()

	old := s.current()
	s.mu.Lock()
	s.settings = in
	cur := s.session
	s.mu.Unlock()

	if cur.eng == nil || !engineChanged(old, in) {
		return s.finish(nil)
	}
	if err := s.stop(); err != nil {
		return s.finish(err)
	}
	return s.finish(s.start(cur.node, cur.ref))
}

func engineChanged(x, y domain.Settings) bool {
	x.ProbeURL, y.ProbeURL = "", ""
	x.RefreshEvery, y.RefreshEvery = 0, 0
	x.Autostart, y.Autostart = "", ""
	x.Emoji, y.Emoji = "", ""
	return !x.Equal(y)
}

func (s *Service) Connect(n domain.Node, ref domain.NodeRef) (Status, error) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	return s.finish(s.start(n, ref))
}

func (s *Service) Disconnect() (Status, error) {
	s.opMu.Lock()
	defer s.opMu.Unlock()

	s.mu.Lock()
	name := s.session.node.Name
	s.mu.Unlock()

	if err := s.clear(); err != nil {
		return s.finish(err)
	}

	if name != "" {
		s.log.Printf("disconnected from %s", name)
	}
	return s.finish(nil)
}

func (s *Service) SetTun(enable bool) (Status, error) {
	s.opMu.Lock()
	defer s.opMu.Unlock()

	s.mu.Lock()
	cur := s.session
	s.mu.Unlock()

	var err error
	if cur.eng != nil && enable != cur.tun {
		if enable {
			err = cur.eng.TunAdd()
		} else {
			err = cur.eng.TunRemove()
		}
	}

	if err != nil {
		if enable && elevate.Needed(err) {
			s.requestRestart()
			err = rpc.ErrElevate
		}
		return s.finish(err)
	}

	s.mu.Lock()
	s.session.tun = enable
	s.mu.Unlock()
	s.mu.Lock()
	s.tun = enable
	s.mu.Unlock()
	return s.finish(nil)
}

func (s *Service) RestartRequested() <-chan struct{} { return s.restart }

func (s *Service) requestRestart() {
	if s.restart == nil {
		return
	}
	select {
	case s.restart <- struct{}{}:
	default:
	}
}

// Shutdown tears the active engine down without broadcasting, for process exit.
func (s *Service) Shutdown() {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if err := s.stop(); err != nil {
		s.log.Print(err)
	}
}

func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status()
}

func (s *Service) status() Status {
	st := Status{Port: s.settings.Port, Tun: s.tun}
	if s.session.eng != nil {
		st.Connected = true
		st.NodeRef, st.NodeName = s.session.ref, s.session.node.Name
		st.Uptime = int64(time.Since(s.session.started).Seconds())
	}
	return st
}

// finish broadcasts the post-op status and passes err through.
func (s *Service) finish(err error) (Status, error) {
	return s.broadcast(), err
}
