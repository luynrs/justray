package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"syscall"

	sbox "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/service"

	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/engine/resolvers"
	"github.com/luynrs/justray/internal/platform/link"
	"github.com/luynrs/justray/internal/platform/wintun"
)

type Box struct {
	lifetime context.Context
	settings domain.Settings
	logPath  string

	inst *sbox.Box
	tun  bool
	node domain.Node
}

func New(ctx context.Context, logPath string) Engine {
	return &Box{lifetime: ctx, logPath: logPath}
}

func (e *Box) Apply(ctx context.Context, spec SessionSpec) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if e.inst == nil {
		return e.start(ctx, spec)
	}
	nodeChanged := !reflect.DeepEqual(e.node, spec.Node)
	tunChanged := spec.Tun != e.tun

	if Rebuilds(e.settings, spec.Settings) || (nodeChanged && tunChanged) {
		if err := e.Stop(); err != nil {
			return err
		}
		return e.start(ctx, spec)
	}
	if nodeChanged {
		if err := e.swap(ctx, spec.Node); err != nil {
			return err
		}
	}
	if tunChanged {
		if spec.Tun {
			if err := e.tunAdd(); err != nil {
				return err
			}
		} else if err := e.tunRemove(); err != nil {
			return err
		}
	}
	e.settings = spec.Settings
	return nil
}

func (e *Box) start(ctx context.Context, spec SessionSpec) error {
	opts, err := Build(ctx, spec.Node, spec.Settings, e.logPath, spec.Tun)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	inst, err := startBox(e.lifetime, *opts)
	if err != nil {
		return err
	}
	e.inst, e.node = inst, spec.Node
	e.settings, e.tun = spec.Settings, spec.Tun
	return nil
}

func startBox(ctx context.Context, opts option.Options) (*sbox.Box, error) {
	for attempt := 0; ; attempt++ {
		inst, err := sbox.New(sbox.Options{Options: opts, Context: Context(ctx)})
		if err == nil {
			err = inst.Start()
		}
		if err == nil {
			return inst, nil
		}
		if inst != nil {
			if closeErr := inst.Close(); closeErr != nil {
				err = errors.Join(err, closeErr)
			}
		}
		if !errors.Is(err, syscall.EBUSY) || attempt == 2 {
			return nil, err
		}
		link.Delete(domain.TunInterface)
	}
}

func (e *Box) swap(ctx context.Context, n domain.Node) error {
	if err := e.apply(ctx, n); err != nil {
		if rbErr := e.apply(ctx, e.node); rbErr != nil {
			_ = e.Stop()
			return errors.Join(err, fmt.Errorf("swap rollback failed: %w", rbErr))
		}
		return err
	}
	e.node = n
	return nil
}

func (e *Box) apply(ctx context.Context, n domain.Node) error {
	ep, obs, err := Proxy(ctx, n, e.settings)
	if err != nil {
		return err
	}

	runtimeCtx := e.runtimeCtx()
	router := e.inst.Router()
	logger := e.inst.LogFactory().NewLogger("outbound/" + Tag)

	_ = e.inst.Endpoint().Remove(Tag)
	_ = e.inst.Outbound().Remove(Tag)
	_ = e.inst.Outbound().Remove(Tag + "-stls")
	if ep != nil {
		return e.inst.Endpoint().Create(runtimeCtx, router, logger, ep.Tag, ep.Type, ep.Options)
	}
	for _, ob := range obs {
		if err := e.inst.Outbound().Create(runtimeCtx, router, logger, ob.Tag, ob.Type, ob.Options); err != nil {
			return err
		}
	}
	return nil
}

func (e *Box) tunAdd() error {
	if _, err := wintun.Ensure(); err != nil {
		return err
	}
	inb := TunInbound(e.settings, resolvers.Get())
	ctx := e.runtimeCtx()
	logger := e.inst.LogFactory().NewLogger("inbound/tun[tun-in]")

	err := e.inst.Inbound().Create(ctx, e.inst.Router(), logger, "tun-in", C.TypeTun, inb.Options)
	if errors.Is(err, syscall.EBUSY) {
		link.Delete(domain.TunInterface)
		err = e.inst.Inbound().Create(ctx, e.inst.Router(), logger, "tun-in", C.TypeTun, inb.Options)
	}
	if err == nil {
		e.tun = true
	}
	return err
}

func (e *Box) tunRemove() error {
	if err := e.inst.Inbound().Remove("tun-in"); err != nil {
		return err
	}
	e.tun = false
	link.Delete(domain.TunInterface)
	return nil
}

func (e *Box) Stop() error {
	if e.inst == nil {
		return nil
	}
	inst := e.inst
	tun := e.tun

	e.inst = nil
	e.tun = false

	err := inst.Close()
	if errors.Is(err, os.ErrClosed) {
		err = nil
	}
	if tun {
		link.Delete(domain.TunInterface)
	}
	return err
}

func (e *Box) Running() bool {
	return e.inst != nil
}

func (e *Box) runtimeCtx() context.Context {
	return service.ContextWith[adapter.NetworkManager](Context(e.lifetime), e.inst.Network())
}
