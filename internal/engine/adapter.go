package engine

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"

	sbox "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	boxsettings "github.com/sagernet/sing-box/common/settings"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/service"

	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/engine/outbound"
	"github.com/luynrs/justray/internal/platform/elevate"
	"github.com/luynrs/justray/internal/platform/link"
)

type box struct {
	lifetime context.Context
	runtime  context.Context
	cancel   context.CancelFunc
	settings domain.Settings
	logPath  string

	inst        *sbox.Box
	tun         bool
	node        domain.Node
	systemProxy boxsettings.SystemProxy
}

func New(ctx context.Context, logPath string) Engine {
	return &box{lifetime: ctx, logPath: logPath}
}

func (e *box) Apply(ctx context.Context, spec Spec) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if spec.Tun && elevate.Needed(syscall.EPERM) {
		return syscall.EPERM
	}
	if e.inst == nil {
		return e.start(ctx, spec)
	}
	nodeChanged := e.node.ID != spec.Node.ID || outbound.ConfigKey(e.node) != outbound.ConfigKey(spec.Node)
	tunChanged := spec.Tun != e.tun

	if rebuilds(e.settings, spec.Settings, spec.Tun) || nodeChanged && tunChanged ||
		(e.systemProxy != nil) != (!spec.Tun && spec.Settings.ProxyType == "system") {
		if err := e.Stop(); err != nil {
			return err
		}
		return e.start(ctx, spec)
	}
	if nodeChanged {
		return e.swap(spec.Node)
	}
	if tunChanged {
		if spec.Tun {
			return e.tunAdd()
		}
		return e.tunRemove()
	}
	return nil
}

func (e *box) start(ctx context.Context, spec Spec) error {
	ctx, finish := context.WithTimeout(ctx, C.StartTimeout)
	defer finish()
	if err := ctx.Err(); err != nil {
		return err
	}

	lifetime, cancel := context.WithCancel(e.lifetime)
	stopStartup := context.AfterFunc(ctx, cancel)
	defer func() {
		stopStartup()
		if e.inst == nil {
			cancel()
		}
	}()
	runtimeCtx := withRegistry(lifetime)
	opts := build(spec.Node, spec.Settings, e.logPath, spec.Tun)
	if runtime.GOOS == "darwin" && os.Geteuid() == 0 && opts.Experimental != nil {
		const directory = "/var/db/justray"
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return fmt.Errorf("prepare rule-set cache: %w", err)
		}
		userID, err := strconv.ParseUint(cmp.Or(os.Getenv("JUSTRAY_UID"), "0"), 10, 32)
		if err != nil {
			return fmt.Errorf("invalid JUSTRAY_UID: %w", err)
		}
		opts.Experimental.CacheFile.Path = filepath.Join(directory, fmt.Sprintf("cache-%d.db", userID))
	}
	inst, err := startBox(runtimeCtx, *opts)
	if err != nil {
		return errors.Join(err, ctx.Err())
	}
	if !stopStartup() {
		_ = inst.Close()
		return ctx.Err()
	}
	if !spec.Tun && spec.Settings.ProxyType == "system" {
		systemProxy, err := boxsettings.NewSystemProxy(runtimeCtx, metadata.ParseSocksaddrHostPort("127.0.0.1", uint16(spec.Settings.Port)), true, nil)
		if err != nil {
			return errors.Join(fmt.Errorf("initialize system proxy: %w", err), inst.Close())
		}
		if err := systemProxy.Enable(); err != nil {
			return errors.Join(fmt.Errorf("set system proxy: %w", err), systemProxy.Disable(), systemProxy.Close(), inst.Close())
		}
		e.systemProxy = systemProxy
	}
	e.inst, e.node = inst, spec.Node
	e.runtime = runtimeCtx
	e.cancel = cancel
	e.settings, e.tun = spec.Settings, spec.Tun
	return nil
}

func startBox(ctx context.Context, opts option.Options) (*sbox.Box, error) {
	for attempt := 0; ; attempt++ {
		inst, err := sbox.New(sbox.Options{Options: opts, Context: ctx})
		if err == nil {
			err = inst.Start()
		}
		if err == nil {
			return inst, nil
		}
		if !errors.Is(err, syscall.EBUSY) || attempt == 2 {
			return nil, err
		}
		link.Delete(domain.Interface)
	}
}

func (e *box) swap(n domain.Node) error {
	managed, _ := e.inst.Outbound().Outbound("proxy")
	proxy := managed.(*proxyOutbound)
	runtime, err := newNodeRuntime(e.runtime, e.inst.Router(), proxy.logger, "proxy-node", n)
	if err != nil {
		return err
	}
	if err := runtime.start(adapter.ListStartStages[1:]...); err != nil {
		_ = runtime.scope.Close()
		return err
	}
	registrar := e.inst.Outbound().(adapter.InternalOutboundRegistrar)
	previous := proxy.current.Load()
	registrar.RemoveInternal(previous.Tag())
	if err := registrar.AddInternal(runtime.Outbound); err != nil {
		_ = registrar.AddInternal(previous.Outbound)
		_ = runtime.scope.Close()
		return err
	}
	if err := proxy.Selector.Start(adapter.StartStateStart, nil); err != nil {
		registrar.RemoveInternal(runtime.Tag())
		_ = registrar.AddInternal(previous.Outbound)
		_ = runtime.scope.Close()
		return err
	}
	proxy.current.Store(runtime)
	_ = previous.scope.Close()
	if invalidator, ok := e.inst.Router().(adapter.ReachabilityInvalidator); ok {
		invalidator.InvalidateReachability()
	}
	if detour(e.settings) != "" {
		for _, transport := range service.FromContext[adapter.DNSTransportManager](e.runtime).Transports() {
			if transport.OutboundTag() == "proxy" {
				transport.Reset()
			}
		}
		service.FromContext[adapter.DNSRouter](e.runtime).ClearCache()
	}
	e.node = n
	return nil
}

func (e *box) tunAdd() error {
	logger := e.inst.LogFactory().NewLogger("inbound/tun[tun-in]")
	inbound, loaded := e.inst.Inbound().Get("tun-in")
	if !loaded {
		if err := e.inst.Inbound().Create(e.runtime, e.inst.Router(), logger, "tun-in", C.TypeTun, tunInbound(e.settings).Options); err != nil {
			return err
		}
		inbound, _ = e.inst.Inbound().Get("tun-in")
	}
	managed := inbound.(*managedTun)
	for attempt := 0; ; attempt++ {
		err := managed.initialize(e.runtime)
		for _, stage := range adapter.ListStartStages[1:] {
			if err == nil {
				err = managed.Start(stage, nil)
			}
		}
		if err == nil {
			e.tun = true
			return nil
		}
		_ = managed.stop()
		if !errors.Is(err, syscall.EBUSY) || attempt == 1 {
			return err
		}
		link.Delete(domain.Interface)
	}
}

func (e *box) tunRemove() error {
	inbound, _ := e.inst.Inbound().Get("tun-in")
	if err := inbound.(*managedTun).stop(); err != nil {
		return err
	}
	e.tun = false
	link.Delete(domain.Interface)
	return nil
}

func (e *box) Stop() error {
	if e.inst == nil {
		return nil
	}
	defer e.cancel()
	inst := e.inst
	tun := e.tun

	e.inst = nil
	e.runtime = nil
	e.cancel = nil
	e.tun = false

	var err error
	if e.systemProxy != nil {
		err = errors.Join(e.systemProxy.Disable(), e.systemProxy.Close())
		e.systemProxy = nil
	}
	if inbound, loaded := inst.Inbound().Get("tun-in"); loaded {
		err = errors.Join(err, inbound.(*managedTun).stop())
	}
	err = errors.Join(err, inst.Close())
	if tun {
		link.Delete(domain.Interface)
	}
	return err
}

func (e *box) Running() bool {
	return e.inst != nil
}
