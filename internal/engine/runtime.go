package engine

import (
	"cmp"
	"context"
	"io"
	"net"
	"sync/atomic"

	"github.com/sagernet/sing-box/adapter"
	boxendpoint "github.com/sagernet/sing-box/adapter/endpoint"
	boxoutbound "github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/common/interrupt"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/service"

	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/engine/outbound"
)

type nodeRuntime struct {
	adapter.Outbound
	ctx       context.Context
	scope     *adapter.Scope
	outbounds *boxoutbound.Manager
	endpoints *boxendpoint.Manager
	interrupt *interrupt.Group
}

func newRuntime(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, node domain.Node) (*nodeRuntime, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	endpoint, outbounds, err := outbound.New(node, tag)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	runtime := &nodeRuntime{
		ctx:       ctx,
		scope:     adapter.NewScope(context.WithoutCancel(ctx), logger),
		interrupt: interrupt.NewGroup(),
	}
	ctx = service.ExtendContext(ctx)
	runtime.endpoints = boxendpoint.NewManager(logger, endpointReg)
	runtime.outbounds = boxoutbound.NewManager(outboundReg, runtime.endpoints, tag)
	service.MustRegister[adapter.EndpointManager](ctx, runtime.endpoints)
	service.MustRegister[adapter.OutboundManager](ctx, runtime.outbounds)
	if endpoint != nil {
		err = runtime.endpoints.Create(ctx, router, logger, endpoint.Tag, endpoint.Type, endpoint.Options)
	}
	for _, outbound := range outbounds {
		if err != nil {
			break
		}
		err = runtime.outbounds.Create(ctx, router, logger, outbound.Tag, outbound.Type, outbound.Options)
	}
	if err == nil {
		runtime.Outbound, _ = runtime.outbounds.Outbound(tag)
		err = runtime.start(adapter.StartStateInitialize)
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		cancel()
		_ = runtime.scope.Close()
		return nil, err
	}
	runtime.scope.Add(func() error { cancel(); return nil })
	return runtime, nil
}

func (runtime *nodeRuntime) start(stages ...adapter.StartStage) error {
	for _, stage := range stages {
		if stage != adapter.StartStateInitialize {
			if err := runtime.ctx.Err(); err != nil {
				return err
			}
		}
		if err := runtime.scope.Start("outbound", runtime.outbounds, stage); err != nil {
			return err
		}
		if err := runtime.scope.Start("endpoint", runtime.endpoints, stage); err != nil {
			return err
		}
	}
	return nil
}

func (runtime *nodeRuntime) close() error {
	runtime.interrupt.Interrupt(true)
	return runtime.scope.Close()
}

type proxyOutbound struct {
	boxoutbound.Adapter
	router  adapter.Router
	logger  log.ContextLogger
	node    domain.Node
	current atomic.Pointer[nodeRuntime]
}

func newProxy(_ context.Context, router adapter.Router, logger log.ContextLogger, tag string, node domain.Node) (adapter.Outbound, error) {
	return &proxyOutbound{
		Adapter: boxoutbound.NewAdapter(C.TypeSelector, tag, nil, nil),
		router:  router,
		logger:  logger,
		node:    node,
	}, nil
}

func (proxy *proxyOutbound) Network() []string                { return proxy.current.Load().Network() }
func (proxy *proxyOutbound) All() []string                    { return []string{proxy.current.Load().Tag()} }
func (proxy *proxyOutbound) Selected(string) adapter.Outbound { return proxy.current.Load().Outbound }
func (proxy *proxyOutbound) AttachConnection(c io.Closer) func() {
	return proxy.current.Load().interrupt.Add(c, true)
}
func (proxy *proxyOutbound) DialContext(ctx context.Context, net string, dst M.Socksaddr) (net.Conn, error) {
	return proxy.current.Load().DialContext(ctx, net, dst)
}
func (proxy *proxyOutbound) ListenPacket(ctx context.Context, dst M.Socksaddr) (net.PacketConn, error) {
	return proxy.current.Load().ListenPacket(ctx, dst)
}
func (proxy *proxyOutbound) swap(ctx, lifetime context.Context, node domain.Node) error {
	runtimeCtx, cancelRuntime := context.WithCancel(lifetime)
	stopSwap := context.AfterFunc(ctx, cancelRuntime)
	runtime, err := newRuntime(runtimeCtx, proxy.router, proxy.logger, "proxy-node", node)
	if err == nil {
		runtime.scope.Add(func() error { cancelRuntime(); return nil })
		err = runtime.start(adapter.ListStartStages[1:]...)
	}
	if !stopSwap() || err != nil {
		cancelRuntime()
		if runtime != nil {
			_ = runtime.close()
		}
		return cmp.Or(ctx.Err(), err)
	}
	_ = proxy.current.Swap(runtime).close()
	return nil
}
func (proxy *proxyOutbound) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	if stage == adapter.StartStateInitialize {
		runtime, err := newRuntime(scope.Context(), proxy.router, proxy.logger, "proxy-node", proxy.node)
		if err != nil {
			return err
		}
		proxy.current.Store(runtime)
		scope.Add(func() error { return proxy.current.Load().close() })
		return nil
	}
	return proxy.current.Load().start(stage)
}

func (proxy *proxyOutbound) InterfaceUpdated(ctx context.Context) {
	if runtime := proxy.current.Load(); runtime != nil {
		for _, outbound := range runtime.outbounds.Outbounds() {
			if listener, ok := outbound.(adapter.InterfaceUpdateListener); ok {
				listener.InterfaceUpdated(ctx)
			}
		}
	}
}

func (proxy *proxyOutbound) CloneEndpoints() []adapter.Endpoint {
	if runtime := proxy.current.Load(); runtime != nil {
		return runtime.endpoints.Endpoints()
	}
	return nil
}

func (proxy *proxyOutbound) SetKeepIdleConnections(keep bool) {
	proxy.forKeepers(func(keeper adapter.IdleConnectionKeeper) { keeper.SetKeepIdleConnections(keep) })
}

func (proxy *proxyOutbound) CloseIdleConnections() {
	proxy.forKeepers(adapter.IdleConnectionKeeper.CloseIdleConnections)
}

func (proxy *proxyOutbound) forKeepers(action func(adapter.IdleConnectionKeeper)) {
	if runtime := proxy.current.Load(); runtime != nil {
		for _, outbound := range runtime.outbounds.Outbounds() {
			if keeper, ok := outbound.(adapter.IdleConnectionKeeper); ok {
				action(keeper)
			}
		}
	}
}
