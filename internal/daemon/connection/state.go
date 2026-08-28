package connection

import (
	"time"

	"github.com/luynrs/justray/internal/daemon/engine"
	"github.com/luynrs/justray/internal/shared/domain"
)

type session struct {
	eng     engine.Engine
	node    domain.Node
	sub     string
	started time.Time
	tun     bool // the tun the engine has
}

// Watch registers a status subscriber
func (s *Service) Watch() (initial Status, ch <-chan Status, cancel func()) {
	c := make(chan Status, 1)
	s.mu.Lock()
	s.watchers[c] = struct{}{}
	initial = s.status()
	s.mu.Unlock()

	return initial, c, func() {
		s.mu.Lock()
		delete(s.watchers, c)
		if len(s.watchers) == 0 {
			clear(s.probes)
		}
		s.mu.Unlock()
	}
}

func (s *Service) broadcast() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.status()
	for ch := range s.watchers {
		select {
		case ch <- st:
		default:
		}
	}
	return st
}
