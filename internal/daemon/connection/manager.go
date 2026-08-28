package connection

import (
	"errors"
	"slices"
	"time"

	"github.com/luynrs/justray/internal/daemon/engine"
	"github.com/luynrs/justray/internal/daemon/platform/elevate"
	"github.com/luynrs/justray/internal/daemon/store"
	"github.com/luynrs/justray/internal/shared/domain"
	"github.com/luynrs/justray/internal/shared/rpc"
)

func (s *Service) Restore() {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	defer s.broadcast()

	id, err := s.store.Active()
	if err != nil || id == "" {
		return
	}
	subs, err := s.store.Subscriptions()
	if err != nil {
		s.log.Print(err)
		return
	}
	n, sub, ok := find(subs, id)
	if !ok {
		return
	}
	if err := s.start(n, sub); err != nil {
		s.log.Print(err)
	}
}

// ForgetIfRemoved drops the active node when its subscription is deleted.
func (s *Service) ForgetIfRemoved(subID string, nodes []domain.Node) {
	s.opMu.Lock()
	defer s.opMu.Unlock()

	gone := func(id string) bool {
		return id != "" && slices.ContainsFunc(nodes, func(n domain.Node) bool { return n.ID == id })
	}
	if active, err := s.store.Active(); err == nil && gone(active) {
		if err := s.store.SetActive(""); err != nil {
			s.log.Print(err)
		}
	}
	if last, err := s.store.Last(); err == nil && gone(last) {
		if err := s.store.SetLast(""); err != nil {
			s.log.Print(err)
		}
	}

	s.mu.Lock()
	live := s.session.sub == subID
	name := s.session.node.Name
	s.mu.Unlock()
	if !live {
		return
	}

	if err := s.clear(); err != nil {
		s.log.Print(err)
		s.setErr(err)
		return
	}
	if name != "" {
		s.log.Printf("disconnected from %s", name)
	}
	s.broadcast()
}

func (s *Service) start(n domain.Node, sub string) (err error) {
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
			s.setErr(err)
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
			s.persistActive(n.ID)
			s.requestRestart()
			err = rpc.ErrElevate
		}
		s.setErr(err)
		return err
	}

	s.mu.Lock()
	s.session = session{eng: eng, node: n, sub: sub, started: time.Now(), tun: tun}
	s.lastErr = ""
	s.mu.Unlock()

	s.persistActive(n.ID)
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
	s.mu.Lock()
	s.lastErr = ""
	s.mu.Unlock()

	if err := s.stop(); err != nil {
		return err
	}
	s.persistActive("")
	return nil
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

func (s *Service) setErr(err error) {
	s.mu.Lock()
	s.lastErr = err.Error()
	s.mu.Unlock()
}

func (s *Service) persistActive(id string) {
	if err := s.store.SetActive(id); err != nil {
		s.log.Print(err)
	}
}

func find(subs []store.Subscription, nodeID string) (domain.Node, string, bool) {
	for _, sub := range subs {
		for _, n := range sub.Nodes {
			if n.ID == nodeID {
				return n, sub.ID, true
			}
		}
	}
	return domain.Node{}, "", false
}
