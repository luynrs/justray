package engine

import (
	"context"
	"sync/atomic"

	"github.com/sagernet/sing-box/adapter"
	boxendpoint "github.com/sagernet/sing-box/adapter/endpoint"
	boxoutbound "github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/group"
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
}

func newNodeRuntime(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, node domain.Node) (runtime *nodeRuntime, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	endpoint, outbounds, err := outbound.New(node, tag)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	runtime = &nodeRuntime{ctx: ctx, scope: adapter.NewScope(context.WithoutCancel(ctx), logger)}
	defer func() {
		if err != nil {
			cancel()
			_ = runtime.scope.Close()
		}
	}()
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
	if err != nil {
		return runtime, err
	}
	runtime.Outbound, _ = runtime.outbounds.Outbound(tag)
	err = runtime.start(adapter.StartStateInitialize)
	if err == nil {
		err = ctx.Err()
	}
	runtime.scope.Add(func() error { cancel(); return nil })
	return runtime, err
}

func (runtime *nodeRuntime) start(stage adapter.StartStage) error {
	if stage != adapter.StartStateInitialize {
		if err := runtime.ctx.Err(); err != nil {
			return err
		}
	}
	if err := runtime.scope.Start("outbound", runtime.outbounds, stage); err != nil {
		return err
	}
	return runtime.scope.Start("endpoint", runtime.endpoints, stage)
}

type proxyOutbound struct {
	*group.Selector
	router  adapter.Router
	logger  log.ContextLogger
	node    domain.Node
	current atomic.Pointer[nodeRuntime]
}

func newProxyOutbound(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, node domain.Node) (adapter.Outbound, error) {
	selector, err := group.NewSelector(ctx, router, logger, tag, option.SelectorOutboundOptions{Outbounds: []string{"proxy-node"}})
	if err != nil {
		return nil, err
	}
	return &proxyOutbound{
		Selector: selector.(*group.Selector), router: router, logger: logger, node: node,
	}, nil
}

func (proxy *proxyOutbound) Dependencies() []string { return nil }

func (proxy *proxyOutbound) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	if stage == adapter.StartStateInitialize {
		runtime, err := newNodeRuntime(scope.Context(), proxy.router, proxy.logger, "proxy-node", proxy.node)
		if err != nil {
			return err
		}
		registrar := service.FromContext[adapter.OutboundManager](scope.Context()).(adapter.InternalOutboundRegistrar)
		if err := registrar.AddInternal(runtime.Outbound); err != nil {
			_ = runtime.scope.Close()
			return err
		}
		proxy.current.Store(runtime)
		scope.Add(func() error {
			runtime := proxy.current.Load()
			registrar.RemoveInternal(runtime.Tag())
			return runtime.scope.Close()
		})
		return nil
	}
	if err := proxy.current.Load().start(stage); err != nil {
		return err
	}
	return proxy.Selector.Start(stage, scope)
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
	if runtime := proxy.current.Load(); runtime != nil {
		for _, outbound := range runtime.outbounds.Outbounds() {
			if keeper, ok := outbound.(adapter.IdleConnectionKeeper); ok {
				keeper.SetKeepIdleConnections(keep)
			}
		}
	}
}

func (proxy *proxyOutbound) CloseIdleConnections() {
	if runtime := proxy.current.Load(); runtime != nil {
		for _, outbound := range runtime.outbounds.Outbounds() {
			if keeper, ok := outbound.(adapter.IdleConnectionKeeper); ok {
				keeper.CloseIdleConnections()
			}
		}
	}
}
