package singbox

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"time"

	sbox "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/service"

	"github.com/luynrs/justray/internal/daemon/engine"
	"github.com/luynrs/justray/internal/daemon/engine/singbox/resolvers"
	"github.com/luynrs/justray/internal/daemon/platform/link"
	"github.com/luynrs/justray/internal/daemon/platform/wintun"
	"github.com/luynrs/justray/internal/shared/domain"
)

type Engine struct {
	settings domain.Settings
	logPath  string

	inst *sbox.Box
	tun  bool
	node domain.Node
}

func New(s domain.Settings, logPath string) engine.Engine {
	return &Engine{settings: s, logPath: logPath}
}

func (e *Engine) Start(n domain.Node, tun bool) error {
	opts, err := Build(n, e.settings, e.logPath, tun)
	if err != nil {
		return err
	}

	var inst *sbox.Box
	err = rideOutEBusy(func() error {
		var last error
		inst, last = newBox(*opts)
		return last
	})
	if err != nil {
		return err
	}

	e.inst, e.tun, e.node = inst, tun, n
	return nil
}

func rideOutEBusy(op func() error) error {
	err := op()
	for i := 0; i < 2 && err != nil && errors.Is(err, syscall.EBUSY); i++ {
		link.Delete(domain.TunInterface)
		waitGone(domain.TunInterface)
		err = op()
	}
	return err
}

func newBox(opts option.Options) (*sbox.Box, error) {
	inst, err := sbox.New(sbox.Options{Options: opts, Context: Context(context.Background())})
	if err == nil {
		err = inst.Start()
	}
	if err != nil {
		if inst != nil {
			_ = inst.Close()
		}
		return nil, err
	}
	return inst, nil
}

func (e *Engine) Swap(n domain.Node) error {
	if err := e.apply(n); err != nil {
		if rbErr := e.apply(e.node); rbErr != nil {
			e.inst.LogFactory().NewLogger("outbound/"+Tag).Error("swap rollback failed, instance left without a proxy outbound: ", rbErr)
		}
		return err
	}
	e.node = n
	return nil
}

func (e *Engine) apply(n domain.Node) error {
	ep, obs, err := Proxy(n, e.settings)
	if err != nil {
		return err
	}

	ctx := e.runtimeCtx()
	router := e.inst.Router()
	logger := e.inst.LogFactory().NewLogger("outbound/" + Tag)

	_ = e.inst.Endpoint().Remove(Tag)
	_ = e.inst.Outbound().Remove(Tag)
	_ = e.inst.Outbound().Remove(Tag + "-stls")
	if ep != nil {
		return e.inst.Endpoint().Create(ctx, router, logger, ep.Tag, ep.Type, ep.Options)
	}
	for _, ob := range obs {
		if err := e.inst.Outbound().Create(ctx, router, logger, ob.Tag, ob.Type, ob.Options); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) TunAdd() error {
	if _, err := wintun.Ensure(); err != nil {
		return err
	}
	inb := TunInbound(e.settings, resolvers.Get())
	ctx := e.runtimeCtx()
	logger := e.inst.LogFactory().NewLogger("inbound/tun[tun-in]")

	err := rideOutEBusy(func() error {
		return e.inst.Inbound().Create(ctx, e.inst.Router(), logger, "tun-in", C.TypeTun, inb.Options)
	})
	if err == nil {
		e.tun = true
	}
	return err
}

func (e *Engine) TunRemove() error {
	err := e.inst.Inbound().Remove("tun-in")
	if err != nil {
		return err
	}
	if waitGone(domain.TunInterface) {
		e.tun = false
		return nil
	}
	link.Delete(domain.TunInterface)
	if !waitGone(domain.TunInterface) {
		return fmt.Errorf("%s still up after removing tun-in", domain.TunInterface)
	}
	e.tun = false
	return nil
}

func (e *Engine) Close() error {
	if e.inst == nil {
		return nil
	}
	err := e.inst.Close()
	if e.tun {
		if !waitGone(domain.TunInterface) {
			link.Delete(domain.TunInterface)
			if !waitGone(domain.TunInterface) {
				return errors.Join(err, fmt.Errorf("%s still up after closing engine", domain.TunInterface))
			}
		}
	}
	if errors.Is(err, os.ErrClosed) {
		err = nil
	}
	if err != nil {
		return err
	}
	e.inst, e.tun = nil, false
	return nil
}

func (e *Engine) runtimeCtx() context.Context {
	return service.ContextWith[adapter.NetworkManager](Context(context.Background()), e.inst.Network())
}

func waitGone(iface string) bool {
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		if _, err := net.InterfaceByName(iface); err != nil {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}
