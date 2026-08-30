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
	if err := s.start(n, ref); err != nil {
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

func (s *Service) start(n domain.Node, ref domain.NodeRef) (err error) {
	if n.TLS != nil && n.TLS.Insecure {
		return errors.New("insecure TLS node is not allowed")
	}
	s.mu.Lock()
	tun, cur := s.tun, s.session
	s.mu.Unlock()

	eng := cur.eng
	hot := cur.eng != nil
	if hot {
		err = cur.eng.Swap(n)
		if err == nil && tun != cur.tun {
			reconcile := cur.eng.TunRemove
			if tun {
				reconcile = cur.eng.TunAdd
			}
			err = reconcile()
		}
	} else {
		if err = s.stop(); err != nil {
			return err
		}

		if err = rpc.ClearLog(rpc.EngineLog(s.dir)); err != nil {
			s.log.Print(err)
		}

		eng = s.newEngine(s.current(), rpc.EngineLog(s.dir))
		if eng == nil {
			err = errors.New("initialize engine: engine is nil")
		} else if err = eng.Start(n, tun); err != nil {
			err = errors.Join(err, s.discard(eng))
		}
	}
	if err != nil {
		if tun && elevate.Needed(err) {
			s.requestRestart()
			err = rpc.ErrElevate
		}
		return err
	}

	s.mu.Lock()
	s.session = session{eng: eng, node: n, ref: ref, started: time.Now(), tun: tun}
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
		if err := cleanup.Close(); err != nil {
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
		if err := eng.Close(); err != nil {
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
	err := eng.Close()
	if err != nil {
		s.mu.Lock()
		s.cleanup = eng
		s.mu.Unlock()
	}
	return err
}
