package engine

import (
	"context"

	sbox "github.com/sagernet/sing-box"
	boxcertificate "github.com/sagernet/sing-box/adapter/certificate"
	"github.com/sagernet/sing-box/adapter/endpoint"
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/adapter/outbound"
	boxservice "github.com/sagernet/sing-box/adapter/service"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/dns"
	"github.com/sagernet/sing-box/dns/transport"
	"github.com/sagernet/sing-box/dns/transport/local"
	"github.com/sagernet/sing-box/option"

	"github.com/sagernet/sing-box/protocol/mixed"

	"github.com/luynrs/justray/internal/domain"
	nodeoutbound "github.com/luynrs/justray/internal/engine/outbound"
)

var ( // read-only
	inboundReg  = inboundRegistry()
	outboundReg = outboundRegistry()
	endpointReg = endpointRegistry()
	dnsReg      = dnsRegistry()
	serviceReg  = boxservice.NewRegistry()
	certReg     = boxcertificate.NewRegistry()
)

func withRegistry(ctx context.Context) context.Context {
	return sbox.Context(ctx, inboundReg, outboundReg, endpointReg, dnsReg, serviceReg, certReg)
}

func inboundRegistry() *inbound.Registry {
	registry := inbound.NewRegistry()
	mixed.RegisterInbound(registry)
	inbound.Register[option.TunInboundOptions](registry, C.TypeTun, newTun)
	return registry
}

func outboundRegistry() *outbound.Registry {
	registry := nodeoutbound.Registry()
	outbound.Register[domain.Node](registry, "justray-node", newProxy)
	return registry
}

func endpointRegistry() *endpoint.Registry { return nodeoutbound.EndpointRegistry() }
func dnsRegistry() *dns.TransportRegistry {
	registry := dns.NewTransportRegistry()
	transport.RegisterUDP(registry)
	transport.RegisterTCP(registry)
	transport.RegisterHTTPS(registry)
	local.RegisterTransport(registry)
	return registry
}
