package engine

import (
	"context"
	"sync"

	"github.com/sagernet/sing-box/adapter"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/tun"
	"github.com/sagernet/sing/service"
)

type tunRuntime struct {
	inbound adapter.Inbound
	scope   *adapter.Scope
}

type managedTun struct {
	router  adapter.Router
	logger  log.ContextLogger
	tag     string
	options option.TunInboundOptions
	access  sync.Mutex
	current *tunRuntime
}

func newManagedTun(_ context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.TunInboundOptions) (adapter.Inbound, error) {
	return &managedTun{router: router, logger: logger, tag: tag, options: options}, nil
}

func (inbound *managedTun) Type() string { return C.TypeTun }
func (inbound *managedTun) Tag() string  { return inbound.tag }

func (inbound *managedTun) initialize(ctx context.Context) error {
	inbound.access.Lock()
	defer inbound.access.Unlock()

	if managed, loaded := service.FromContext[adapter.OutboundManager](ctx).Outbound("proxy"); loaded {
		ctx = service.ExtendContext(ctx)
		service.MustRegister[adapter.EndpointManager](ctx, managed.(*proxyOutbound).current.Load().endpoints)
	}
	scope := adapter.NewScope(ctx, inbound.logger)
	component, err := tun.NewInbound(scope.Context(), inbound.router, inbound.logger, inbound.tag, inbound.options)
	if err == nil {
		err = component.Start(adapter.StartStateInitialize, scope)
	}
	if err != nil {
		_ = scope.Close()
		return err
	}
	inbound.current = &tunRuntime{inbound: component, scope: scope}
	return nil
}

func (inbound *managedTun) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	if stage == adapter.StartStateInitialize {
		scope.Add(inbound.stop)
		return inbound.initialize(scope.Context())
	}
	inbound.access.Lock()
	defer inbound.access.Unlock()
	runtime := inbound.current
	return runtime.inbound.Start(stage, runtime.scope)
}

func (inbound *managedTun) stop() error {
	inbound.access.Lock()
	defer inbound.access.Unlock()
	if runtime := inbound.current; runtime != nil {
		inbound.current = nil
		return runtime.scope.Close()
	}
	return nil
}

func (inbound *managedTun) InterfaceUpdated(ctx context.Context) {
	inbound.access.Lock()
	defer inbound.access.Unlock()
	if runtime := inbound.current; runtime != nil {
		if listener, ok := runtime.inbound.(adapter.InterfaceUpdateListener); ok {
			listener.InterfaceUpdated(ctx)
		}
	}
}
