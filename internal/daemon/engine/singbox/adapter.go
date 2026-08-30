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
	settings domain.Settings
	logPath  string

	inst *sbox.Box
	tun  bool
	name string
	node domain.Node
}

func New(logPath string) engine.Engine {
	return &Engine{logPath: logPath}
}

func (e *Engine) Apply(spec engine.SessionSpec) error {
	if e.inst == nil {
		return e.start(spec)
	}
	if engine.Rebuilds(e.settings, spec.Settings) {
		if err := e.Stop(); err != nil {
			return err
		}
		return e.start(spec)
	}
	if !reflect.DeepEqual(e.node, spec.Node) {
		if err := e.swap(spec.Node); err != nil {
			return err
		}
	}
	if spec.Tun != e.tun {
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

func (e *Engine) start(spec engine.SessionSpec) error {
	opts, err := Build(spec.Node, spec.Settings, e.logPath, spec.Tun)
	if err != nil {
		return err
	}
	var before map[string]struct{}
	if spec.Tun && runtime.GOOS == "darwin" {
		before = interfaceNames()
	}

	inst, err := startBox(*opts)
	if inst != nil {
		e.inst, e.node = inst, spec.Node
		if spec.Tun && runtime.GOOS == "darwin" {
			e.name = newInterface(before)
			if e.name == "" {
				return errors.Join(errors.New("tun interface name unavailable"), e.Stop())
			}
		}
		e.settings, e.tun = spec.Settings, spec.Tun
	}
	return err
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

func startBox(opts option.Options) (*sbox.Box, error) {
	for attempt := 0; ; attempt++ {
		inst, err := sbox.New(sbox.Options{Options: opts, Context: Context(context.Background())})
		if err == nil {
			err = inst.Start()
		}
		if err == nil {
			return inst, nil
		}
		if inst != nil {
			if closeErr := inst.Close(); closeErr != nil {
				return inst, errors.Join(err, closeErr)
			}
		}
		if !errors.Is(err, syscall.EBUSY) || attempt == 2 {
			return nil, err
		}
		link.Delete(domain.TunInterface)
		waitGone(domain.TunInterface)
	}
}

func (e *Engine) swap(n domain.Node) error {
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
				_ = e.inst.Inbound().Remove("tun-in")
				return errors.New("tun interface name unavailable")
			}
		}
		e.tun = true
	}
	return err
}

func (e *Engine) tunRemove() error {
	err := e.inst.Inbound().Remove("tun-in")
	if err != nil {
		return err
	}
	iface := e.interfaceName()
	if iface == "" {
		return errors.New("tun interface name unavailable")
	}
	if !waitGone(iface) {
		link.Delete(iface)
		if !waitGone(iface) {
			return fmt.Errorf("%s still up after removing tun-in", iface)
		}
	}
	e.tun = false
	e.name = ""
	return nil
}

func (e *Engine) Stop() error {
	if e.inst == nil {
		return nil
	}
	err := e.inst.Close()
	if errors.Is(err, os.ErrClosed) {
		err = nil
	}
	if e.tun {
		iface := e.interfaceName()
		if iface == "" {
			err = errors.Join(err, errors.New("tun interface name unavailable"))
		} else if !waitGone(iface) {
			link.Delete(iface)
			if !waitGone(iface) {
				err = errors.Join(err, fmt.Errorf("%s still up after closing engine", iface))
			}
		}
	}
	if err != nil {
		return err
	}
	e.inst, e.tun, e.name = nil, false, ""
	return nil
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
