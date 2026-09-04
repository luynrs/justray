package singbox

import (
	"context"

	sbox "github.com/sagernet/sing-box"
	boxcertificate "github.com/sagernet/sing-box/adapter/certificate"
	"github.com/sagernet/sing-box/adapter/endpoint"
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/adapter/outbound"
	boxservice "github.com/sagernet/sing-box/adapter/service"
	"github.com/sagernet/sing-box/dns"
	"github.com/sagernet/sing-box/dns/transport"
	"github.com/sagernet/sing-box/dns/transport/local"
	"github.com/sagernet/sing-box/protocol/anytls"
	"github.com/sagernet/sing-box/protocol/direct"
	"github.com/sagernet/sing-box/protocol/http"
	"github.com/sagernet/sing-box/protocol/hysteria"
	"github.com/sagernet/sing-box/protocol/hysteria2"
	"github.com/sagernet/sing-box/protocol/mixed"
	"github.com/sagernet/sing-box/protocol/shadowsocks"
	"github.com/sagernet/sing-box/protocol/shadowtls"
	"github.com/sagernet/sing-box/protocol/socks"
	"github.com/sagernet/sing-box/protocol/trojan"
	"github.com/sagernet/sing-box/protocol/tuic"
	"github.com/sagernet/sing-box/protocol/tun"
	"github.com/sagernet/sing-box/protocol/vless"
	"github.com/sagernet/sing-box/protocol/vmess"
	"github.com/sagernet/sing-box/protocol/wireguard"
	_ "github.com/sagernet/sing-box/transport/v2rayxhttp"
)

var ( // read-only, built once instead of on every connect/probe
	inboundReg  = inboundRegistry()
	outboundReg = outboundRegistry()
	endpointReg = endpointRegistry()
	dnsReg      = dnsTransportRegistry()
	serviceReg  = boxservice.NewRegistry()
	certReg     = boxcertificate.NewRegistry()
)

func Context(ctx context.Context) context.Context {
	return sbox.Context(ctx, inboundReg, outboundReg, endpointReg, dnsReg, serviceReg, certReg)
}

func inboundRegistry() *inbound.Registry {
	registry := inbound.NewRegistry()
	mixed.RegisterInbound(registry)
	tun.RegisterInbound(registry)
	return registry
}

func outboundRegistry() *outbound.Registry {
	registry := outbound.NewRegistry()
	direct.RegisterOutbound(registry)
	vless.RegisterOutbound(registry)
	vmess.RegisterOutbound(registry)
	trojan.RegisterOutbound(registry)
	shadowsocks.RegisterOutbound(registry)
	shadowtls.RegisterOutbound(registry)
	hysteria.RegisterOutbound(registry)
	hysteria2.RegisterOutbound(registry)
	tuic.RegisterOutbound(registry)
	anytls.RegisterOutbound(registry)
	socks.RegisterOutbound(registry)
	http.RegisterOutbound(registry)
	return registry
}

func endpointRegistry() *endpoint.Registry {
	registry := endpoint.NewRegistry()
	wireguard.RegisterEndpoint(registry)
	return registry
}

func dnsTransportRegistry() *dns.TransportRegistry {
	registry := dns.NewTransportRegistry()
	transport.RegisterUDP(registry)
	transport.RegisterTCP(registry)
	transport.RegisterHTTPS(registry)
	local.RegisterTransport(registry)
	return registry
}
