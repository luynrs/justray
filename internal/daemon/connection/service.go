package connection

import (
	"context"
	"errors"
	"log"
	"sync/atomic"
	"time"

	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/engine"
	"github.com/luynrs/justray/internal/ipc"
	"github.com/luynrs/justray/internal/platform/elevate"
)

// Core serializes engine operations. Readers only access the published status.
type Service struct {
	ctx       context.Context
	newEngine engine.NewFunc
	probeAll  engine.ProbeFunc
	log       *log.Logger
	dir       string

	eng     engine.Engine
	status  atomic.Pointer[ipc.Status]
	restart chan struct{}
}

func New(ctx context.Context, dir string, newEngine engine.NewFunc, probe engine.ProbeFunc, logger *log.Logger) *Service {
	return &Service{
		ctx:       ctx,
		newEngine: newEngine,
		probeAll:  probe,
		log:       logger,
		dir:       dir,
		restart:   make(chan struct{}, 1),
	}
}

func (s *Service) Connect(ctx context.Context, n domain.Node, ref domain.NodeRef, settings domain.Settings, tun bool) error {
	return s.requestElevation(s.apply(ctx, n, ref, settings, tun, true), tun)
}

func (s *Service) Apply(ctx context.Context, n domain.Node, ref domain.NodeRef, settings domain.Settings, tun bool) error {
	return s.requestElevation(s.apply(ctx, n, ref, settings, tun, false), tun)
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
		s.log.Printf("disconnected from %s", name)
	}
	return nil
}

func (s *Service) Restore(n domain.Node, ref domain.NodeRef, settings domain.Settings, tun bool) {
	err := s.apply(s.ctx, n, ref, settings, tun, true)
	if tun && elevate.Needed(err) {
		s.log.Printf("tun requires elevation, starting in proxy mode (port %d)", settings.Port)
		err = s.apply(s.ctx, n, ref, settings, false, true)
	}
	if err != nil {
		s.log.Print(err)
	}
}

func (s *Service) ForgetIfRemoved(subID string) error {
	if s.Status().NodeRef.SubscriptionID != subID {
		return nil
	}
	return s.Disconnect(context.Background())
}

func (s *Service) Probe(ctx context.Context, nodes []domain.Node, settings domain.Settings, onResult func(string, engine.Result)) error {
	return s.probeAll(ctx, nodes, settings, ipc.EngineLog(s.dir), onResult)
}

func (s *Service) RestartRequested() <-chan struct{} { return s.restart }

func (s *Service) Shutdown() {
	if err := s.stop(); err != nil {
		s.log.Print(err)
	}
}

func (s *Service) Status() ipc.Status {
	if st := s.status.Load(); st != nil {
		return *st
	}
	return ipc.Status{}
}

func (s *Service) apply(ctx context.Context, n domain.Node, ref domain.NodeRef, settings domain.Settings, tun, resetStarted bool) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if n.TLS != nil && n.TLS.Insecure {
		return errors.New("insecure TLS node is not allowed")
	}

	previous := s.Status()
	eng := s.eng
	started := previous.StartedAt

	if eng == nil {
		if err = ipc.ClearLog(ipc.EngineLog(s.dir)); err != nil {
			s.log.Print(err)
		}
		eng = s.newEngine(s.ctx, ipc.EngineLog(s.dir))
		if eng == nil {
			err = errors.New("initialize engine: engine is nil")
		} else if err = eng.Apply(ctx, engine.SessionSpec{Node: n, Settings: settings, Tun: tun}); err != nil {
			err = errors.Join(err, eng.Stop())
		}
	} else {
		err = eng.Apply(ctx, engine.SessionSpec{Node: n, Settings: settings, Tun: tun})
	}
	if err != nil {
		if eng != nil && !eng.Running() {
			s.eng = nil
			s.status.Store(nil)
		}
		return err
	}

	if resetStarted || started.IsZero() {
		started = time.Now()
	}
	s.eng = eng
	s.status.Store(&ipc.Status{Connected: true, NodeRef: ref, NodeName: n.Name, StartedAt: started, Tun: tun, Port: settings.Port})
	if previous.NodeRef != ref || resetStarted {
		s.log.Printf("connected to %s (%s %s:%d)", n.Name, n.Protocol, n.Server, n.Port)
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

func (s *Service) requestElevation(err error, tun bool) error {
	if tun && elevate.Needed(err) {
		select {
		case s.restart <- struct{}{}:
		default:
		}
		return ipc.ErrElevate
	}
	return err
}
