package engine

import (
	"context"
	"net/netip"
	"net/url"
	"strconv"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/json/badoption"

	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/engine/outbound"
)

const (
	Tag             = "proxy"
	maxProbeNodes   = 512
	maxProbeWorkers = 32
)

var dnsStrategy = map[string]option.DomainStrategy{
	"auto": option.DomainStrategy(C.DomainStrategyPreferIPv4),
	"ipv4": option.DomainStrategy(C.DomainStrategyIPv4Only),
	"ipv6": option.DomainStrategy(C.DomainStrategyIPv6Only),
}

func Build(ctx context.Context, n domain.Node, s domain.Settings, logPath string, tun bool) (*option.Options, error) {
	ep, obs, err := outbound.New(n, Tag)
	if err != nil {
		return nil, err
	}

	opts := &option.Options{
		Log: &option.LogOptions{Level: s.LogLevel, Output: logPath},
		Inbounds: []option.Inbound{
			{Type: C.TypeMixed, Tag: "mixed-in", Options: &option.HTTPMixedInboundOptions{
				ListenOptions: option.ListenOptions{
					Listen:     common.Ptr(badoption.Addr(listenAddr(s))),
					ListenPort: uint16(s.Port),
				},
			}},
		},
		Outbounds: []option.Outbound{
			{Type: C.TypeDirect, Tag: "direct", Options: &option.DirectOutboundOptions{}},
		},
		DNS: &option.DNSOptions{RawDNSOptions: option.RawDNSOptions{
			DNSClientOptions: option.DNSClientOptions{Strategy: dnsStrategy[s.IPVersion]},
			Servers:          dnsServers(s, detour(s), "remote"),
			Final:            "remote",
		}},
		Route: &option.RouteOptions{
			Final:                 final(s),
			AutoDetectInterface:   true,
			DefaultDomainResolver: &option.DomainResolveOptions{Server: "remote", Strategy: dnsStrategy[s.IPVersion]},
			Rules:                 rules(s),
		},
	}
	if detour(s) != "" {
		opts.DNS.Servers = append(opts.DNS.Servers, dnsServers(s, "", "node")...)
		opts.Route.DefaultDomainResolver.Server = "node"
	}
	attach(opts, ep, obs)
	if tun {
		opts.Inbounds = append(opts.Inbounds, TunInbound(s))
	}
	return opts, nil
}

func ProbeTag(i int) string { return "p" + strconv.Itoa(i) }

func ProbeConfig(ctx context.Context, nodes []domain.Node, s domain.Settings, logPath string) *option.Options {
	opts := &option.Options{
		Log: &option.LogOptions{Level: s.LogLevel, Output: logPath},
		Route: &option.RouteOptions{
			AutoDetectInterface:   true,
			DefaultDomainResolver: &option.DomainResolveOptions{Server: "remote", Strategy: dnsStrategy[s.IPVersion]},
		},
		Outbounds: []option.Outbound{{Type: C.TypeDirect, Tag: "direct", Options: &option.DirectOutboundOptions{}}},
		DNS: &option.DNSOptions{RawDNSOptions: option.RawDNSOptions{
			DNSClientOptions: option.DNSClientOptions{Strategy: dnsStrategy[s.IPVersion]},
			Servers:          dnsServers(s, "", "remote"),
			Final:            "remote",
		}},
	}
	for i, n := range nodes {
		if ep, obs, err := outbound.New(n, ProbeTag(i)); err == nil {
			attach(opts, ep, obs)
		}
	}
	return opts
}

func attach(opts *option.Options, ep *option.Endpoint, obs []option.Outbound) {
	if ep != nil {
		opts.Endpoints = append(opts.Endpoints, *ep)
	}
	opts.Outbounds = append(opts.Outbounds, obs...)
}

func detour(s domain.Settings) string {
	if final(s) != Tag {
		return ""
	}
	host := s.DNS
	if u, err := url.Parse(s.DNS); err == nil && u.Hostname() != "" {
		host = u.Hostname()
	} else if ap, err := netip.ParseAddrPort(s.DNS); err == nil {
		host = ap.Addr().String()
	}
	if host == "localhost" {
		return ""
	}
	if addr, err := netip.ParseAddr(host); err == nil && (addr.IsLoopback() || addr.IsLinkLocalUnicast() || (s.BypassLocal == "on" && addr.IsPrivate())) {
		return ""
	}
	return Tag
}

func dnsServers(s domain.Settings, detourTag, tag string) []option.DNSServerOptions {
	dns := s.DNS
	if dns == "" {
		dns = domain.DefaultDNS
	}
	remote := option.RemoteDNSServerOptions{
		RawLocalDNSServerOptions: option.RawLocalDNSServerOptions{
			DialerOptions: option.DialerOptions{Detour: detourTag},
		},
		DNSServerAddressOptions: option.DNSServerAddressOptions{Server: dns},
	}
	transportType := C.DNSTypeUDP
	if detourTag != "" {
		transportType = C.DNSTypeTCP
	}
	u, _ := url.Parse(dns) // Settings.Normalize validates the URL
	if u != nil && u.Hostname() != "" {
		transportType = C.DNSTypeHTTPS
		remote.Server = u.Hostname()
		if u.Port() != "" {
			port, _ := strconv.ParseUint(u.Port(), 10, 16)
			remote.ServerPort = uint16(port)
		}
	} else if address, err := netip.ParseAddrPort(dns); err == nil {
		remote.Server = address.Addr().String()
		remote.ServerPort = address.Port()
	}
	if transportType != C.DNSTypeHTTPS {
		return []option.DNSServerOptions{{Type: transportType, Tag: tag, Options: &remote}}
	}
	if _, err := netip.ParseAddr(remote.Server); err != nil {
		remote.DomainResolver = &option.DomainResolveOptions{Server: tag + "-bootstrap", Strategy: dnsStrategy[s.IPVersion]}
	}
	servers := []option.DNSServerOptions{
		{
			Type: C.DNSTypeHTTPS,
			Tag:  tag,
			Options: &option.RemoteHTTPSDNSServerOptions{
				RemoteTLSDNSServerOptions: option.RemoteTLSDNSServerOptions{
					RemoteDNSServerOptions: remote,
				},
				Path: u.EscapedPath(),
			},
		},
	}
	if remote.DomainResolver != nil {
		servers = append(servers, option.DNSServerOptions{Type: C.DNSTypeLocal, Tag: tag + "-bootstrap", Options: &option.LocalDNSServerOptions{}})
	}
	return servers
}

func listenAddr(s domain.Settings) netip.Addr {
	if s.AllowLAN == "on" {
		return netip.IPv4Unspecified()
	}
	return netip.MustParseAddr("127.0.0.1")
}
