package connection

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/luynrs/justray/internal/daemon/engine"
	"github.com/luynrs/justray/internal/daemon/platform/elevate"
	"github.com/luynrs/justray/internal/shared/domain"
	"github.com/luynrs/justray/internal/shared/rpc"
)

type session struct {
	eng     engine.Engine
	node    domain.Node
	ref     domain.NodeRef
	started time.Time
	tun     bool
}

type Service struct {
	ctx       context.Context
	newEngine engine.New
	probeAll  engine.Probe
	log       *log.Logger
	dir       string

	session session
	cleanup engine.Engine
	restart chan struct{}
}

func New(ctx context.Context, dir string, newEngine engine.New, probe engine.Probe, logger *log.Logger) *Service {
	return &Service{
		ctx:       ctx,
		newEngine: newEngine,
		probeAll:  probe,
		log:       logger,
		dir:       dir,
		restart:   make(chan struct{}, 1),
	}
}

func (s *Service) Connect(ctx context.Context, n domain.Node, ref domain.NodeRef, settings domain.Settings, tun bool) (rpc.Status, error) {
	if err := ctx.Err(); err != nil {
		return s.Status(), err
	}
	return s.Status(), s.apply(n, ref, settings, tun, true)
}

func (s *Service) Apply(ctx context.Context, n domain.Node, ref domain.NodeRef, settings domain.Settings, tun bool) (rpc.Status, error) {
	if err := ctx.Err(); err != nil {
		return s.Status(), err
	}
	return s.Status(), s.apply(n, ref, settings, tun, false)
}

func (s *Service) Disconnect(ctx context.Context) (rpc.Status, error) {
	if err := ctx.Err(); err != nil {
		return s.Status(), err
	}
	name := s.session.node.Name
	if err := s.stop(ctx); err != nil {
		return s.Status(), err
	}
	if name != "" {
		s.log.Printf("disconnected from %s", name)
	}
	return s.Status(), nil
}

func (s *Service) Restore(n domain.Node, ref domain.NodeRef, settings domain.Settings, tun bool) {
	if err := s.apply(n, ref, settings, tun, true); err != nil {
		s.log.Print(err)
	}
}

func (s *Service) ForgetIfRemoved(subID string) {
	if s.session.ref.SubscriptionID != subID {
		return
	}
	name := s.session.node.Name
	if err := s.stop(s.ctx); err != nil {
		s.log.Print(err)
		return
	}
	if name != "" {
		s.log.Printf("disconnected from %s", name)
	}
}

func (s *Service) Probe(ctx context.Context, nodes []domain.Node, settings domain.Settings) (map[string]engine.Result, error) {
	return s.probeAll(ctx, nodes, settings, rpc.EngineLog(s.dir))
}

func (s *Service) RestartRequested() <-chan struct{} { return s.restart }

func (s *Service) requestRestart() {
	select {
	case s.restart <- struct{}{}:
	default:
	}
}

func (s *Service) Shutdown() {
	if err := s.stop(s.ctx); err != nil {
		s.log.Print(err)
	}
}

func (s *Service) Status() rpc.Status {
	st := rpc.Status{}
	if s.session.eng != nil {
		st.Connected = true
		st.NodeRef, st.NodeName = s.session.ref, s.session.node.Name
		st.Uptime = int64(time.Since(s.session.started).Seconds())
	}
	return st
}

func (s *Service) apply(n domain.Node, ref domain.NodeRef, settings domain.Settings, tun, resetStarted bool) (err error) {
	if n.TLS != nil && n.TLS.Insecure {
		return errors.New("insecure TLS node is not allowed")
	}
	eng := s.session.eng
	if eng == nil {
		if err = s.stop(s.ctx); err != nil {
			return err
		}
		if err = rpc.ClearLog(rpc.EngineLog(s.dir)); err != nil {
			s.log.Print(err)
		}
		eng = s.newEngine(rpc.EngineLog(s.dir))
		if eng == nil {
			err = errors.New("initialize engine: engine is nil")
		} else if err = eng.Apply(s.ctx, engine.SessionSpec{Node: n, Settings: settings, Tun: tun}); err != nil {
			err = errors.Join(err, s.discard(s.ctx, eng))
		}
	} else {
		err = eng.Apply(s.ctx, engine.SessionSpec{Node: n, Settings: settings, Tun: tun})
	}
	if err != nil {
		if tun && elevate.Needed(err) {
			s.requestRestart()
			err = rpc.ErrElevate
		}
		return err
	}

	started := s.session.started
	if resetStarted || started.IsZero() {
		started = time.Now()
	}
	s.session = session{eng: eng, node: n, ref: ref, started: started, tun: tun}
	s.log.Printf("connected to %s (%s %s:%d)", n.Name, n.Protocol, n.Server, n.Port)
	return nil
}

func (s *Service) stop(ctx context.Context) error {
	var errs []error
	if s.cleanup != nil {
		if err := s.cleanup.Stop(ctx); err != nil {
			errs = append(errs, err)
		} else {
			s.cleanup = nil
		}
	}
	if s.session.eng != nil && s.session.eng != s.cleanup {
		if err := s.session.eng.Stop(ctx); err != nil {
			errs = append(errs, err)
		} else {
			s.session = session{}
		}
	}
	return errors.Join(errs...)
}

func (s *Service) discard(ctx context.Context, eng engine.Engine) error {
	err := eng.Stop(ctx)
	if err != nil {
		s.cleanup = eng
	}
	return err
}
