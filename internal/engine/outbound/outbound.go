package outbound

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"

	boxendpoint "github.com/sagernet/sing-box/adapter/endpoint"
	boxoutbound "github.com/sagernet/sing-box/adapter/outbound"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/anytls"
	"github.com/sagernet/sing-box/protocol/direct"
	"github.com/sagernet/sing-box/protocol/http"
	"github.com/sagernet/sing-box/protocol/hysteria"
	"github.com/sagernet/sing-box/protocol/hysteria2"
	"github.com/sagernet/sing-box/protocol/shadowsocks"
	"github.com/sagernet/sing-box/protocol/shadowtls"
	"github.com/sagernet/sing-box/protocol/socks"
	"github.com/sagernet/sing-box/protocol/trojan"
	"github.com/sagernet/sing-box/protocol/tuic"
	"github.com/sagernet/sing-box/protocol/vless"
	"github.com/sagernet/sing-box/protocol/vmess"
	protocolwireguard "github.com/sagernet/sing-box/protocol/wireguard"
	SJSON "github.com/sagernet/sing/common/json"
	"github.com/sagernet/sing/service"

	"github.com/luynrs/justray/internal/domain"
)

func New(n domain.Node, tag string) (*option.Endpoint, []option.Outbound, error) {
	if len(n.Native) > 0 {
		return native(n, tag)
	}
	if !domain.ValidPort(n.Port) {
		return nil, nil, fmt.Errorf("%s: port out of range: %d", n.Protocol, n.Port)
	}
	transportOptions, err := transport(n)
	if err != nil {
		return nil, nil, err
	}
	if n.Protocol == domain.WG {
		ep, err := wireguard(n, tag)
		return ep, nil, err
	}
	if n.Protocol == domain.Shadow {
		if n.ShadowTLS == nil {
			return nil, nil, errors.New("shadowtls: missing settings")
		}
		return nil, []option.Outbound{shadowTLS(n, tag)}, nil
	}

	out, err := proxy(n, tag, transportOptions)
	if err != nil {
		return nil, nil, err
	}

	var obs []option.Outbound
	if ss, ok := out.Options.(*option.ShadowsocksOutboundOptions); ok && n.ShadowTLS != nil {
		ss.Detour = tag + "-stls"
		obs = append(obs, shadowTLS(n, ss.Detour))
	}
	return nil, append(obs, *out), nil
}

func proxy(n domain.Node, tag string, transportOptions *option.V2RayTransportOptions) (*option.Outbound, error) {
	tls := tlsOptions(n)
	if tls == nil {
		switch n.Protocol {
		case domain.HY1, domain.HY2, domain.TUIC, domain.AnyTLS:
			tls = &option.OutboundTLSOptions{Enabled: true}
		}
	}

	switch n.Protocol {
	case domain.VLESS:
		var packetEncoding *string
		if n.PacketEncoding != "" {
			packetEncoding = &n.PacketEncoding
		}
		return &option.Outbound{Type: C.TypeVLESS, Tag: tag, Options: &option.VLESSOutboundOptions{
			ServerOptions:               server(n),
			UUID:                        n.Auth.UUID,
			Flow:                        n.Auth.Flow,
			PacketEncoding:              packetEncoding,
			OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{TLS: tls},
			Transport:                   transportOptions,
		}}, nil

	case domain.VMess:
		return &option.Outbound{Type: C.TypeVMess, Tag: tag, Options: &option.VMessOutboundOptions{
			ServerOptions:               server(n),
			UUID:                        n.Auth.UUID,
			Security:                    n.Auth.Method,
			AlterId:                     n.Auth.AlterID,
			PacketEncoding:              cmp.Or(n.PacketEncoding, "xudp"),
			OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{TLS: tls},
			Transport:                   transportOptions,
		}}, nil

	case domain.Trojan:
		return &option.Outbound{Type: C.TypeTrojan, Tag: tag, Options: &option.TrojanOutboundOptions{
			ServerOptions:               server(n),
			Password:                    n.Auth.Password,
			OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{TLS: tls},
			Transport:                   transportOptions,
		}}, nil

	case domain.SS:
		return &option.Outbound{Type: C.TypeShadowsocks, Tag: tag, Options: &option.ShadowsocksOutboundOptions{
			ServerOptions: server(n),
			Method:        n.Auth.Method,
			Password:      n.Auth.Password,
		}}, nil

	case domain.HY2:
		var obfs *option.Hysteria2Obfs
		if n.Obfs != "" {
			obfs = &option.Hysteria2Obfs{Type: n.Obfs, Password: n.ObfsPassword}
		}
		return &option.Outbound{Type: C.TypeHysteria2, Tag: tag, Options: &option.Hysteria2OutboundOptions{
			ServerOptions:               server(n),
			UpMbps:                      n.UpMbps,
			DownMbps:                    n.DownMbps,
			Password:                    n.Auth.Password,
			Obfs:                        obfs,
			OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{TLS: tls},
		}}, nil

	case domain.HY1:
		return &option.Outbound{Type: C.TypeHysteria, Tag: tag, Options: &option.HysteriaOutboundOptions{
			ServerOptions:               server(n),
			AuthString:                  n.Auth.Password,
			UpMbps:                      n.UpMbps,
			DownMbps:                    n.DownMbps,
			Obfs:                        n.ObfsPassword,
			OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{TLS: tls},
		}}, nil

	case domain.TUIC:
		return &option.Outbound{Type: C.TypeTUIC, Tag: tag, Options: &option.TUICOutboundOptions{
			ServerOptions:               server(n),
			UUID:                        n.Auth.UUID,
			Password:                    n.Auth.Password,
			CongestionControl:           n.Congestion,
			UDPRelayMode:                n.UDPRelayMode,
			OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{TLS: tls},
		}}, nil

	case domain.AnyTLS:
		return &option.Outbound{Type: C.TypeAnyTLS, Tag: tag, Options: &option.AnyTLSOutboundOptions{
			ServerOptions:               server(n),
			Password:                    n.Auth.Password,
			OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{TLS: tls},
		}}, nil

	case domain.SOCKS:
		return &option.Outbound{Type: C.TypeSOCKS, Tag: tag, Options: &option.SOCKSOutboundOptions{
			ServerOptions: server(n),
			Username:      n.Auth.Username,
			Password:      n.Auth.Password,
		}}, nil

	case domain.HTTP:
		return &option.Outbound{Type: C.TypeHTTP, Tag: tag, Options: &option.HTTPOutboundOptions{
			ServerOptions:               server(n),
			Username:                    n.Auth.Username,
			Password:                    n.Auth.Password,
			OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{TLS: tls},
		}}, nil
	}
	return nil, fmt.Errorf("unsupported protocol: %s", n.Protocol)
}

func shadowTLS(n domain.Node, tag string) option.Outbound {
	tls := tlsOptions(n)
	if tls == nil {
		tls = &option.OutboundTLSOptions{Enabled: true, ServerName: n.ShadowTLS.SNI}
	}
	return option.Outbound{Type: C.TypeShadowTLS, Tag: tag, Options: &option.ShadowTLSOutboundOptions{
		ServerOptions: server(n),
		Version:       n.ShadowTLS.Version,
		Password:      n.ShadowTLS.Password,
		OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{
			TLS: tls,
		},
	}}
}

func wireguard(n domain.Node, tag string) (*option.Endpoint, error) {
	w := n.WireGuard
	if w == nil || w.PrivateKey == "" || len(w.Address) == 0 {
		return nil, errors.New("wireguard: no key material")
	}
	address := make([]netip.Prefix, 0, len(w.Address))
	for _, s := range w.Address {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return nil, fmt.Errorf("wireguard: %w", err)
		}
		address = append(address, p)
	}
	var keepalive option.AWGRange
	if w.PersistentKeepaliveInterval != nil {
		keepalive = *w.PersistentKeepaliveInterval
	}
	return &option.Endpoint{Type: C.TypeWireGuard, Tag: tag, Options: &option.WireGuardEndpointOptions{
		AmneziaWGOptions: w.AmneziaWGOptions,
		Address:          address,
		PrivateKey:       w.PrivateKey,
		MTU:              w.MTU,
		Peers: []option.WireGuardPeer{{
			Address:                     n.Server,
			Port:                        uint16(n.Port),
			PublicKey:                   w.PeerPublicKey,
			PreSharedKey:                w.PreSharedKey,
			AllowedIPs:                  []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("::/0")},
			Reserved:                    w.Reserved,
			PersistentKeepaliveInterval: keepalive,
		}},
	}}, nil
}

func server(n domain.Node) option.ServerOptions {
	return option.ServerOptions{Server: n.Server, ServerPort: uint16(n.Port)}
}

func Registry() *boxoutbound.Registry {
	registry := boxoutbound.NewRegistry()
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

func EndpointRegistry() *boxendpoint.Registry {
	registry := boxendpoint.NewRegistry()
	protocolwireguard.RegisterEndpoint(registry)
	return registry
}

var Context = service.ContextWith[option.EndpointOptionsRegistry](
	service.ContextWith[option.OutboundOptionsRegistry](context.Background(), Registry()), EndpointRegistry())

func validateNativeOptions(options any) error {
	if wrapper, ok := options.(option.OutboundTLSOptionsWrapper); ok {
		if tls := wrapper.TakeOutboundTLSOptions(); tls != nil && tls.Enabled && tls.Insecure {
			return errors.New("insecure TLS is not supported")
		}
	}
	if wrapper, ok := options.(option.DialerOptionsWrapper); ok {
		if resolver := wrapper.TakeDialerOptions().DomainResolver; resolver != nil && resolver.Server != "" {
			return errors.New("unsupported native domain_resolver reference")
		}
	}
	return nil
}

func ConfigKey(node domain.Node) string {
	endpoint, outbounds, err := New(node, "node")
	if err == nil {
		config := option.Options{Outbounds: outbounds}
		if endpoint != nil {
			config.Endpoints = []option.Endpoint{*endpoint}
		}
		for index := range outbounds {
			keyDefaults(outbounds[index].Options)
		}
		if data, err := SJSON.MarshalContext(Context, config); err == nil {
			return string(data)
		}
	}
	node.ID, node.Name = "", ""
	data, _ := json.Marshal(node)
	return string(data)
}

// Normalize equivalent options from current import formats for comparison only.
// Runtime options retain their original values.
func keyDefaults(options any) {
	switch settings := options.(type) {
	case *option.ShadowsocksOutboundOptions:
		if settings.Detour != "" && settings.Plugin == "" {
			// The supported ShadowTLS detour ignores the SS destination.
			settings.ServerOptions = option.ServerOptions{}
		}
	case *option.HysteriaOutboundOptions:
		if settings.AuthString != "" {
			settings.Auth = []byte(settings.AuthString)
		}
		settings.AuthString = ""
	case *option.VLESSOutboundOptions:
		if settings.PacketEncoding != nil && *settings.PacketEncoding == "xudp" {
			settings.PacketEncoding = nil
		}
	case *option.VMessOutboundOptions:
		if settings.Security == "auto" {
			settings.Security = ""
		}
	case *option.SOCKSOutboundOptions:
		if settings.Version == "5" {
			settings.Version = ""
		}
	}
	server, hasServer := options.(option.ServerOptionsWrapper)
	tls, hasTLS := options.(option.OutboundTLSOptionsWrapper)
	if hasServer && hasTLS {
		if settings := tls.TakeOutboundTLSOptions(); settings != nil && settings.Enabled && settings.ServerName == server.TakeServerOptions().Server {
			settings.ServerName = ""
		}
	}
}

func native(node domain.Node, tag string) (*option.Endpoint, []option.Outbound, error) {
	if node.Protocol == domain.WG {
		var endpoint option.Endpoint
		if err := SJSON.UnmarshalContext(Context, node.Native[0], &endpoint); err != nil {
			return nil, nil, err
		}
		endpoint.Tag = tag
		return &endpoint, nil, validateNativeOptions(endpoint.Options)
	}
	var outbounds []option.Outbound
	for index, raw := range node.Native {
		var outbound option.Outbound
		if err := SJSON.UnmarshalContext(Context, raw, &outbound); err != nil {
			return nil, nil, err
		}
		if err := validateNativeOptions(outbound.Options); err != nil {
			return nil, nil, err
		}
		outbound.Tag = tag
		if index > 0 {
			outbound.Tag += "-stls"
		}
		if wrapper, ok := outbound.Options.(option.DialerOptionsWrapper); ok {
			dialer := wrapper.TakeDialerOptions()
			if dialer.Detour != "" {
				if index != 0 || dialer.Detour != "node-stls" || len(node.Native) != 2 {
					return nil, nil, errors.New("unsupported native outbound detour")
				}
				dialer.Detour = tag + "-stls"
				wrapper.ReplaceDialerOptions(dialer)
			}
		}
		outbounds = append(outbounds, outbound)
	}
	if len(outbounds) == 2 {
		outbounds[0], outbounds[1] = outbounds[1], outbounds[0]
	}
	return nil, outbounds, nil
}
