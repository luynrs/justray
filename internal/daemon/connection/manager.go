package connection

import (
	"errors"
	"time"

	"github.com/luynrs/justray/internal/daemon/engine"
	"github.com/luynrs/justray/internal/daemon/platform/elevate"
	"github.com/luynrs/justray/internal/shared/domain"
	"github.com/luynrs/justray/internal/shared/rpc"
)

func (s *Service) Restore(n domain.Node, ref domain.NodeRef) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	defer s.broadcast()
	s.mu.Lock()
	settings, tun := s.settings, s.tun
	s.mu.Unlock()
	if err := s.apply(n, ref, settings, tun, true); err != nil {
		s.log.Print(err)
	}
}

// ForgetIfRemoved drops the live connection when its subscription is deleted.
func (s *Service) ForgetIfRemoved(subID string) {
	s.opMu.Lock()
	defer s.opMu.Unlock()

	s.mu.Lock()
	live := s.session.ref.SubscriptionID == subID
	name := s.session.node.Name
	s.mu.Unlock()
	if !live {
		return
	}

	if err := s.clear(); err != nil {
		s.log.Print(err)
		return
	}
	if name != "" {
		s.log.Printf("disconnected from %s", name)
	}
	s.broadcast()
}

func (s *Service) apply(n domain.Node, ref domain.NodeRef, settings domain.Settings, tun, resetStarted bool) (err error) {
	if n.TLS != nil && n.TLS.Insecure {
		return errors.New("insecure TLS node is not allowed")
	}
	s.mu.Lock()
	cur := s.session
	s.mu.Unlock()
	eng := cur.eng
	if eng == nil {
		if err = s.stop(); err != nil {
			return err
		}

		if err = rpc.ClearLog(rpc.EngineLog(s.dir)); err != nil {
			s.log.Print(err)
		}

		eng = s.newEngine(rpc.EngineLog(s.dir))
		if eng == nil {
			err = errors.New("initialize engine: engine is nil")
		} else if err = eng.Apply(engine.SessionSpec{Node: n, Settings: settings, Tun: tun}); err != nil {
			err = errors.Join(err, s.discard(eng))
		}
	} else {
		err = eng.Apply(engine.SessionSpec{Node: n, Settings: settings, Tun: tun})
	}
	if err != nil {
		if tun && elevate.Needed(err) {
			s.requestRestart()
			err = rpc.ErrElevate
		}
		return err
	}

	started := cur.started
	if resetStarted || started.IsZero() {
		started = time.Now()
	}
	s.mu.Lock()
	s.session = session{eng: eng, node: n, ref: ref, started: started, tun: tun}
	s.mu.Unlock()

	s.log.Printf("connected to %s (%s %s:%d)", n.Name, n.Protocol, n.Server, n.Port)
	return nil
}

func (s *Service) stop() error {
	s.mu.Lock()
	cleanup, eng := s.cleanup, s.session.eng
	s.mu.Unlock()
	var errs []error
	if cleanup != nil {
		if err := cleanup.Stop(); err != nil {
			errs = append(errs, err)
		} else {
			s.mu.Lock()
			if s.cleanup == cleanup {
				s.cleanup = nil
			}
			s.mu.Unlock()
		}
	}
	if eng != nil && eng != cleanup {
		if err := eng.Stop(); err != nil {
			errs = append(errs, err)
		} else {
			s.mu.Lock()
			if s.session.eng == eng {
				s.session = session{}
			}
			s.mu.Unlock()
		}
	}
	return errors.Join(errs...)
}

func (s *Service) clear() error {
	return s.stop()
}

func (s *Service) discard(eng engine.Engine) error {
	err := eng.Stop()
	if err != nil {
		s.mu.Lock()
		s.cleanup = eng
		s.mu.Unlock()
	}
	return err
}
