package connection

import (
	"log"
	"sync"
	"time"

	"github.com/luynrs/justray/internal/daemon/engine"
	"github.com/luynrs/justray/internal/daemon/platform/autostart"
	"github.com/luynrs/justray/internal/daemon/platform/elevate"
	"github.com/luynrs/justray/internal/daemon/store"
	"github.com/luynrs/justray/internal/shared/domain"
	"github.com/luynrs/justray/internal/shared/rpc"
)

type Status = rpc.Status

type Service struct {
	store     store.Disk
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

func New(dir string, st store.Disk, newEngine engine.New, probe engine.Probe, logger *log.Logger) *Service {
	state, err := st.State()
	if err != nil {
		logger.Print(err)
	}
	settings, err := state.Settings.Normalize()
	if err != nil {
		logger.Print(err)
		settings, _ = domain.Settings{}.Normalize()
	}
	settings.Autostart = toggle(autostart.Enabled())
	return &Service{
		store:     st,
		newEngine: newEngine,
		probeAll:  probe,
		log:       logger,
		dir:       dir,
		tun:       state.Tun,
		settings:  settings,
		probes:    map[domain.NodeRef]engine.Result{},
		watchers:  map[chan Status]struct{}{},
		restart:   make(chan struct{}, 1),
	}
}

func (s *Service) Settings() domain.Settings { return s.current() }

func (s *Service) current() domain.Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settings
}

// RefreshEvery is the subscription refresh interval in hours, 0 when off.
func (s *Service) RefreshEvery() int { return s.current().RefreshEvery }

func toggle(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

func (s *Service) SetSettings(in domain.Settings) (Status, error) {
	s.opMu.Lock()
	defer s.opMu.Unlock()

	in, err := in.Normalize()
	if err != nil {
		return s.Status(), err
	}

	old := s.current()
	if in.Autostart != old.Autostart {
		apply := autostart.Enable
		if in.Autostart == "off" {
			apply = autostart.Disable
		}
		if err := apply(); err != nil {
			s.log.Print(err)
			return s.Status(), err
		}
	}
	if err := s.store.SetSettings(in); err != nil {
		return s.Status(), err
	}

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

func (s *Service) Connect(queryID, subID string) (Status, error) {
	s.opMu.Lock()
	defer s.opMu.Unlock()

	subs, err := s.store.Subscriptions()
	if err != nil {
		return Status{}, err
	}
	n, ref, err := find(subs, domain.NodeRef{SubscriptionID: subID, NodeID: queryID})
	if err != nil {
		return Status{}, err
	}

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
	return s.commitTun(enable)
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

func (s *Service) commitTun(enable bool) (Status, error) {
	if err := s.store.SetTun(enable); err != nil {
		return s.finish(err)
	}
	s.mu.Lock()
	s.tun = enable
	s.mu.Unlock()
	return s.finish(nil)
}

// Shutdown tears the active engine down without broadcasting, for process exit.
func (s *Service) Shutdown() {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if err := s.stop(); err != nil {
		s.log.Print(err)
	}
}

// ActiveRef returns the connected node, the last one used, or zero if none.
func (s *Service) ActiveRef() (domain.NodeRef, error) {
	ref, err := s.store.Active()
	if ref.NodeID != "" || err != nil {
		return ref, err
	}
	return s.store.Last()
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
