package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"

	sbox "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/service"

	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/engine/outbound"
	"github.com/luynrs/justray/internal/platform/link"
)

type box struct {
	lifetime context.Context
	runtime  context.Context
	settings domain.Settings
	logPath  string

	inst *sbox.Box
	tun  bool
	node domain.Node
}

func New(ctx context.Context, logPath string) Engine {
	return &box{lifetime: ctx, logPath: logPath}
}

func (e *box) Apply(ctx context.Context, spec Spec) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if e.inst == nil {
		return e.start(ctx, spec)
	}
	nodeChanged := e.node.ID != spec.Node.ID || e.node.ConfigKey() != spec.Node.ConfigKey()
	tunChanged := spec.Tun != e.tun

	if rebuilds(e.settings, spec.Settings, spec.Tun) || nodeChanged && tunChanged {
		if err := e.Stop(); err != nil {
			return err
		}
		return e.start(ctx, spec)
	}
	if nodeChanged {
		if err := e.swap(spec.Node); err != nil {
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
	return nil
}

func (e *box) start(ctx context.Context, spec Spec) error {
	opts, err := build(spec.Node, spec.Settings, e.logPath, spec.Tun)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	runtimeCtx := withRegistry(e.lifetime)
	inst, err := startBox(runtimeCtx, *opts)
	if err != nil {
		return err
	}
	e.inst, e.node = inst, spec.Node
	e.runtime = runtimeCtx
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
		link.Delete(domain.TunInterface)
	}
}

func (e *box) swap(n domain.Node) error {
	if err := e.apply(n); err != nil {
		if rbErr := e.apply(e.node); rbErr != nil {
			_ = e.Stop()
			return errors.Join(err, fmt.Errorf("swap rollback failed: %w", rbErr))
		}
		return err
	}
	e.node = n
	return nil
}

func (e *box) apply(n domain.Node) error {
	endpoint, outbounds, err := outbound.New(n, "proxy-node")
	if err != nil {
		return err
	}

	router := e.inst.Router()
	logger := e.inst.LogFactory().NewLogger("outbound/proxy")

	_ = e.inst.Outbound().Remove("proxy")
	_ = e.inst.Endpoint().Remove("proxy-node")
	_ = e.inst.Outbound().Remove("proxy-node")
	_ = e.inst.Outbound().Remove("proxy-node-stls")
	if endpoint != nil {
		if err := e.inst.Endpoint().Create(e.runtime, router, logger, endpoint.Tag, endpoint.Type, endpoint.Options); err != nil {
			return err
		}
	}
	for _, ob := range outbounds {
		if err := e.inst.Outbound().Create(e.runtime, router, logger, ob.Tag, ob.Type, ob.Options); err != nil {
			return err
		}
	}
	if err := e.inst.Outbound().Create(e.runtime, router, logger, "proxy", C.TypeSelector, &option.SelectorOutboundOptions{Outbounds: []string{"proxy-node"}}); err != nil {
		return err
	}
	if detour(e.settings) != "" {
		servers := dnsServers(e.settings, detour(e.settings), "remote")
		for i := len(servers) - 1; i >= 0; i-- {
			if err := service.FromContext[adapter.DNSTransportManager](e.runtime).Create(e.runtime, e.inst.LogFactory().NewLogger("dns/"+servers[i].Tag), servers[i].Tag, servers[i].Type, servers[i].Options); err != nil {
				return err
			}
		}
		service.FromContext[adapter.DNSRouter](e.runtime).ClearCache()
	}
	return nil
}

func (e *box) tunAdd() error {
	inb := tunInbound(e.settings)
	logger := e.inst.LogFactory().NewLogger("inbound/tun[tun-in]")

	err := e.inst.Inbound().Create(e.runtime, e.inst.Router(), logger, "tun-in", C.TypeTun, inb.Options)
	if errors.Is(err, syscall.EBUSY) {
		link.Delete(domain.TunInterface)
		err = e.inst.Inbound().Create(e.runtime, e.inst.Router(), logger, "tun-in", C.TypeTun, inb.Options)
	}
	if err == nil {
		e.tun = true
	}
	return err
}

func (e *box) tunRemove() error {
	if err := e.inst.Inbound().Remove("tun-in"); err != nil {
		return err
	}
	e.tun = false
	link.Delete(domain.TunInterface)
	return nil
}

func (e *box) Stop() error {
	if e.inst == nil {
		return nil
	}
	inst := e.inst
	tun := e.tun

	e.inst = nil
	e.runtime = nil
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

func (e *box) Running() bool {
	return e.inst != nil
}
