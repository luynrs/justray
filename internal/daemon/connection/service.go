package connection

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/sagernet/sing-box/log"

	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/engine"
	"github.com/luynrs/justray/internal/ipc"
	"github.com/luynrs/justray/internal/platform/elevate"
)

// Serialize engine operations
type Service struct {
	ctx       context.Context
	newEngine func(context.Context, string) engine.Engine
	dir       string

	eng    engine.Engine
	status atomic.Pointer[ipc.Status]
}

func New(ctx context.Context, dir string, newEngine func(context.Context, string) engine.Engine) *Service {
	return &Service{
		ctx:       ctx,
		newEngine: newEngine,
		dir:       dir,
	}
}

func (s *Service) Connect(ctx context.Context, n domain.Node, ref domain.NodeRef, settings domain.Settings, tun bool) error {
	return s.apply(ctx, n, ref, settings, tun, true)
}

func (s *Service) Apply(ctx context.Context, n domain.Node, ref domain.NodeRef, settings domain.Settings, tun bool) error {
	return s.apply(ctx, n, ref, settings, tun, false)
}

func (s *Service) Disconnect(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	name := s.Status().NodeName
	if err := s.stop(); err != nil {
		return err
	}
	if name != "" {
		log.Info("disconnected from ", name)
	}
	return nil
}

func (s *Service) Restore(n domain.Node, ref domain.NodeRef, settings domain.Settings, tun bool) error {
	err := s.apply(s.ctx, n, ref, settings, tun, true)
	if errors.Is(err, ipc.ErrElevate) {
		log.Info("restoring TUN requires elevation")
		return err
	}
	if err != nil {
		s.SetError(err)
	}
	return err
}

func (s *Service) SetError(err error) {
	status := s.Status()
	status.Error = ""
	if err != nil {
		log.Error("cannot restore connection: ", err)
		status.Error = err.Error()
	}
	s.status.Store(&status)
}

func (s *Service) Probe(ctx context.Context, nodes []domain.Node, settings domain.Settings, onResult func(int, engine.Result)) error {
	instance := s.eng
	if instance == nil {
		instance = s.newEngine(s.ctx, ipc.EngineLog(s.dir))
	}
	return instance.Probe(ctx, nodes, settings, onResult)
}

func (s *Service) Shutdown() {
	if err := s.stop(); err != nil {
		log.Error("cannot stop engine: ", err)
	}
}

func (s *Service) Status() ipc.Status {
	if st := s.status.Load(); st != nil {
		return *st
	}
	return ipc.Status{}
}

func (s *Service) apply(ctx context.Context, n domain.Node, ref domain.NodeRef, settings domain.Settings, tun, resetStarted bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if n.TLS != nil && n.TLS.Insecure {
		return errors.New("insecure TLS is not supported")
	}

	previous := s.Status()
	eng := s.eng
	started := previous.StartedAt
	creating := eng == nil

	if creating {
		if err := ipc.ClearLog(ipc.EngineLog(s.dir)); err != nil {
			log.Warn("cannot clear engine log: ", err)
		}
		eng = s.newEngine(s.ctx, ipc.EngineLog(s.dir))
	}
	if err := eng.Apply(ctx, engine.Spec{Node: n, Settings: settings, Tun: tun}); err != nil {
		if creating {
			err = errors.Join(err, eng.Stop())
		}
		if !eng.Running() {
			s.eng = nil
			s.status.Store(nil)
		}
		if tun && elevate.Needed(err) {
			return ipc.ErrElevate
		}
		return err
	}

	if resetStarted || started.IsZero() {
		started = time.Now()
	}
	s.eng = eng
	s.status.Store(&ipc.Status{Connected: true, NodeRef: ref, NodeName: n.Name, StartedAt: started, Tun: tun, Port: settings.Port})
	if previous.NodeRef != ref || resetStarted {
		log.Info("connected to ", n.Name, " (", n.ProtocolName(), ", ", domain.ServerAddress(n.Server, n.Port), ")")
	}
	return nil
}

func (s *Service) stop() error {
	eng := s.eng
	s.eng = nil
	s.status.Store(nil)
	if eng != nil {
		return eng.Stop()
	}
	return nil
}
