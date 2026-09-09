package outbound

import (
	"cmp"
	"fmt"
	"net/netip"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"

	"github.com/luynrs/justray/internal/domain"
)

func New(n domain.Node, tag string) (*option.Endpoint, []option.Outbound, error) {
	if !domain.ValidPort(n.Port) {
		return nil, nil, fmt.Errorf("%s: port %d out of range", n.Protocol, n.Port)
	}
	if n.Protocol == domain.WG {
		ep, err := wireguard(n, tag)
		return ep, nil, err
	}
	if n.Protocol == domain.Shadow {
		if n.ShadowTLS == nil {
			return nil, nil, fmt.Errorf("shadowtls: missing settings")
		}
		return nil, []option.Outbound{shadowTLS(n, tag)}, nil
	}

	out, err := proxy(n, tag)
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

func TLSOnly(p domain.Proto) bool {
	switch p {
	case domain.HY1, domain.HY2, domain.TUIC, domain.AnyTLS:
		return true
	}
	return false
}

func proxy(n domain.Node, tag string) (*option.Outbound, error) {
	tls := tlsOptions(n)
	if tls == nil && TLSOnly(n.Protocol) {
		tls = &option.OutboundTLSOptions{Enabled: true}
	}

	switch n.Protocol {
	case domain.VLess:
		pe := packetEncoding(n)
		return &option.Outbound{Type: C.TypeVLESS, Tag: tag, Options: &option.VLESSOutboundOptions{
			ServerOptions:               server(n),
			UUID:                        n.Auth.UUID,
			Flow:                        n.Auth.Flow,
			PacketEncoding:              &pe,
			OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{TLS: tls},
			Transport:                   transport(n),
		}}, nil

	case domain.VMess:
		return &option.Outbound{Type: C.TypeVMess, Tag: tag, Options: &option.VMessOutboundOptions{
			ServerOptions:               server(n),
			UUID:                        n.Auth.UUID,
			Security:                    cmp.Or(n.Auth.Method, "auto"),
			AlterId:                     n.Auth.AlterID,
			PacketEncoding:              packetEncoding(n),
			OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{TLS: tls},
			Transport:                   transport(n),
		}}, nil

	case domain.Trojan:
		return &option.Outbound{Type: C.TypeTrojan, Tag: tag, Options: &option.TrojanOutboundOptions{
			ServerOptions:               server(n),
			Password:                    n.Auth.Password,
			OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{TLS: tls},
			Transport:                   transport(n),
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
			Version:       "5",
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
	return nil, fmt.Errorf("unsupported protocol %q", n.Protocol)
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
		return nil, fmt.Errorf("wireguard: no key material")
	}
	address := make([]netip.Prefix, 0, len(w.Address))
	for _, s := range w.Address {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return nil, fmt.Errorf("wireguard: %w", err)
		}
		address = append(address, p)
	}
	return &option.Endpoint{Type: C.TypeWireGuard, Tag: tag, Options: &option.WireGuardEndpointOptions{
		Address:    address,
		PrivateKey: w.PrivateKey,
		MTU:        w.MTU,
		Peers: []option.WireGuardPeer{{
			Address:                     n.Server,
			Port:                        uint16(n.Port),
			PublicKey:                   w.PeerPublicKey,
			PreSharedKey:                w.PreSharedKey,
			AllowedIPs:                  []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("::/0")},
			Reserved:                    w.Reserved,
			PersistentKeepaliveInterval: "25",
		}},
	}}, nil
}

func server(n domain.Node) option.ServerOptions {
	return option.ServerOptions{Server: n.Server, ServerPort: uint16(n.Port)}
}

func packetEncoding(n domain.Node) string { return cmp.Or(n.PacketEncoding, "xudp") }
