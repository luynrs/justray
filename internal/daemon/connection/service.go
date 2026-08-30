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
	port    int
}

type Service struct {
	ctx       context.Context
	newEngine engine.New
	probeAll  engine.Probe
	log       *log.Logger
	dir       string

	session session
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
	err := s.apply(ctx, n, ref, settings, tun, true)
	return s.Status(), err
}

func (s *Service) Apply(ctx context.Context, n domain.Node, ref domain.NodeRef, settings domain.Settings, tun bool) (rpc.Status, error) {
	if err := ctx.Err(); err != nil {
		return s.Status(), err
	}
	err := s.apply(ctx, n, ref, settings, tun, false)
	return s.Status(), err
}

func (s *Service) Disconnect(ctx context.Context) (rpc.Status, error) {
	if err := ctx.Err(); err != nil {
		return s.Status(), err
	}
	name := s.session.node.Name
	err := s.stop(ctx)
	if err != nil {
		return s.Status(), err
	}
	if name != "" {
		s.log.Printf("disconnected from %s", name)
	}
	return s.Status(), nil
}

func (s *Service) Restore(n domain.Node, ref domain.NodeRef, settings domain.Settings, tun bool) {
	if err := s.apply(s.ctx, n, ref, settings, tun, true); err != nil {
		s.log.Print(err)
	}
}

func (s *Service) ForgetIfRemoved(subID string) error {
	if s.session.ref.SubscriptionID != subID {
		return nil
	}
	name := s.session.node.Name
	err := s.stop(context.WithoutCancel(s.ctx))
	if err != nil {
		s.log.Print(err)
		return err
	}
	if name != "" {
		s.log.Printf("disconnected from %s", name)
	}
	return nil
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
	if err := s.stop(context.WithoutCancel(s.ctx)); err != nil {
		s.log.Print(err)
	}
}

func (s *Service) Status() rpc.Status {
	st := rpc.Status{}
	if s.session.eng != nil && s.session.eng.Running() {
		st.Connected = true
		st.NodeRef, st.NodeName = s.session.ref, s.session.node.Name
		st.Uptime = int64(time.Since(s.session.started).Seconds())
		st.Tun = s.session.tun
		st.Port = s.session.port
	}
	return st
}

func (s *Service) apply(ctx context.Context, n domain.Node, ref domain.NodeRef, settings domain.Settings, tun, resetStarted bool) (err error) {
	if n.TLS != nil && n.TLS.Insecure {
		return errors.New("insecure TLS node is not allowed")
	}
	eng := s.session.eng
	if eng == nil {
		if err = s.stop(context.WithoutCancel(s.ctx)); err != nil {
			return err
		}
		if err = rpc.ClearLog(rpc.EngineLog(s.dir)); err != nil {
			s.log.Print(err)
		}
		eng = s.newEngine(s.ctx, rpc.EngineLog(s.dir))
		if eng == nil {
			err = errors.New("initialize engine: engine is nil")
		} else if err = eng.Apply(ctx, engine.SessionSpec{Node: n, Settings: settings, Tun: tun}); err != nil {
			err = errors.Join(err, s.discard(context.WithoutCancel(s.ctx), eng))
		}
	} else {
		err = eng.Apply(ctx, engine.SessionSpec{Node: n, Settings: settings, Tun: tun})
	}
	if err != nil {
		if eng != nil && !eng.Running() {
			s.session = session{}
		}
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
	s.session = session{eng: eng, node: n, ref: ref, started: started, tun: tun, port: settings.Port}
	s.log.Printf("connected to %s (%s %s:%d)", n.Name, n.Protocol, n.Server, n.Port)
	return nil
}

func (s *Service) stop(ctx context.Context) error {
	if eng := s.session.eng; eng != nil {
		s.session = session{}
		return eng.Stop(ctx)
	}
	return nil
}

func (s *Service) discard(ctx context.Context, eng engine.Engine) error {
	return eng.Stop(ctx)
}
