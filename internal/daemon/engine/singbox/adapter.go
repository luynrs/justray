package singbox

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"reflect"
	"runtime"
	"strings"
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
	lifetime context.Context
	settings domain.Settings
	logPath  string

	inst *sbox.Box
	tun  bool
	name string
	node domain.Node
}

func New(ctx context.Context, logPath string) engine.Engine {
	return &Engine{lifetime: ctx, logPath: logPath}
}

func (e *Engine) Apply(ctx context.Context, spec engine.SessionSpec) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if e.inst == nil {
		return e.start(ctx, spec)
	}
	nodeChanged := !reflect.DeepEqual(e.node, spec.Node)
	tunChanged := spec.Tun != e.tun

	if engine.Rebuilds(e.settings, spec.Settings) || (nodeChanged && tunChanged) {
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

func (e *Engine) start(ctx context.Context, spec engine.SessionSpec) error {
	opts, err := Build(ctx, spec.Node, spec.Settings, e.logPath, spec.Tun)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var before map[string]struct{}
	if spec.Tun && runtime.GOOS == "darwin" {
		before = interfaceNames()
	}

	inst, err := startBox(e.lifetime, *opts)
	if err != nil {
		return err
	}
	e.inst, e.node = inst, spec.Node
	e.settings, e.tun = spec.Settings, spec.Tun
	if spec.Tun && runtime.GOOS == "darwin" {
		e.name = newInterface(before)
		if e.name == "" {
			return errors.Join(errors.New("tun interface name unavailable"), e.Stop())
		}
	}
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
		waitGone(domain.TunInterface)
	}
}

func (e *Engine) swap(ctx context.Context, n domain.Node) error {
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

func (e *Engine) apply(ctx context.Context, n domain.Node) error {
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

func (e *Engine) tunAdd() error {
	var before map[string]struct{}
	if runtime.GOOS == "darwin" {
		before = interfaceNames()
	}
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
		if runtime.GOOS == "darwin" {
			e.name = newInterface(before)
			if e.name == "" {
				if rbErr := e.inst.Inbound().Remove("tun-in"); rbErr != nil {
					_ = e.Stop()
					return errors.Join(errors.New("tun interface name unavailable"), fmt.Errorf("tun rollback failed: %w", rbErr))
				}
				return errors.New("tun interface name unavailable")
			}
		}
		e.tun = true
	}
	return err
}

func (e *Engine) tunRemove() error {
	if err := e.inst.Inbound().Remove("tun-in"); err != nil {
		return err
	}
	e.tun = false
	iface := e.interfaceName()
	e.name = ""
	var errs []error
	if iface == "" {
		errs = append(errs, errors.New("tun interface name unavailable"))
	} else if !waitGone(iface) {
		link.Delete(iface)
		if !waitGone(iface) {
			errs = append(errs, fmt.Errorf("%s still up after removing tun-in", iface))
		}
	}
	if len(errs) > 0 {
		_ = e.Stop()
		return errors.Join(errs...)
	}
	return nil
}

func (e *Engine) Stop() error {
	if e.inst == nil {
		return nil
	}
	inst := e.inst
	tun := e.tun
	iface := e.interfaceName()

	e.inst = nil
	e.tun = false
	e.name = ""

	err := inst.Close()
	if errors.Is(err, os.ErrClosed) {
		err = nil
	}
	if tun {
		if iface == "" {
			err = errors.Join(err, errors.New("tun interface name unavailable"))
		} else if !waitGone(iface) {
			link.Delete(iface)
			if !waitGone(iface) {
				err = errors.Join(err, fmt.Errorf("%s still up after closing engine", iface))
			}
		}
	}
	return err
}

func (e *Engine) Running() bool {
	return e.inst != nil
}

func (e *Engine) interfaceName() string {
	if runtime.GOOS == "darwin" {
		return e.name
	}
	return domain.TunInterface
}

func interfaceNames() map[string]struct{} {
	before := map[string]struct{}{}
	interfaces, _ := net.Interfaces()
	for _, iface := range interfaces {
		before[iface.Name] = struct{}{}
	}
	return before
}

func newInterface(before map[string]struct{}) string {
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		interfaces, _ := net.Interfaces()
		for _, iface := range interfaces {
			if strings.HasPrefix(iface.Name, "utun") {
				if _, ok := before[iface.Name]; !ok {
					return iface.Name
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	return ""
}

func (e *Engine) runtimeCtx() context.Context {
	return service.ContextWith[adapter.NetworkManager](Context(e.lifetime), e.inst.Network())
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
