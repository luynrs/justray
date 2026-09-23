package engine

import (
	"context"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

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
	ep, obs, err := Proxy(ctx, n, s)
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
			Servers:          dnsServers(s, detour(s)),
			Final:            "remote",
		}},
		Route: &option.RouteOptions{
			Final:               final(s),
			AutoDetectInterface: true,
			Rules:               rules(s),
		},
	}
	attach(opts, ep, obs)
	if tun {
		opts.Inbounds = append(opts.Inbounds, TunInbound(s))
	}
	return opts, nil
}

func Proxy(ctx context.Context, n domain.Node, s domain.Settings) (*option.Endpoint, []option.Outbound, error) {
	n, err := resolved(ctx, n, s)
	if err != nil {
		return nil, nil, err
	}
	return outbound.New(n, Tag)
}

func ProbeTag(i int) string { return "p" + strconv.Itoa(i) }

func ProbeConfig(ctx context.Context, nodes []domain.Node, s domain.Settings, logPath string) *option.Options {
	opts := &option.Options{
		Log:   &option.LogOptions{Level: s.LogLevel, Output: logPath},
		Route: &option.RouteOptions{AutoDetectInterface: true},
		Outbounds: []option.Outbound{
			{Type: C.TypeDirect, Tag: "direct", Options: &option.DirectOutboundOptions{}},
		},
		DNS: &option.DNSOptions{RawDNSOptions: option.RawDNSOptions{
			DNSClientOptions: option.DNSClientOptions{Strategy: dnsStrategy[s.IPVersion]},
			Servers: []option.DNSServerOptions{
				{Type: C.DNSTypeLocal, Tag: "local", Options: &option.LocalDNSServerOptions{PreferGo: true}},
			},
			Final: "local",
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
	if final(s) == Tag {
		return Tag
	}
	return ""
}

func dnsServers(s domain.Settings, detour string) []option.DNSServerOptions {
	dns := s.DNS
	if dns == "" {
		dns = domain.DefaultDNS
	}
	remote := option.RemoteDNSServerOptions{
		RawLocalDNSServerOptions: option.RawLocalDNSServerOptions{
			DialerOptions: option.DialerOptions{Detour: detour},
		},
		DNSServerAddressOptions: option.DNSServerAddressOptions{Server: dns},
	}
	if !strings.HasPrefix(dns, "https://") {
		if ap, err := netip.ParseAddrPort(dns); err == nil {
			remote.Server = ap.Addr().String()
			remote.ServerPort = ap.Port()
		}
		return []option.DNSServerOptions{{Type: C.DNSTypeUDP, Tag: "remote", Options: &remote}}
	}

	u, _ := url.Parse(dns) // Settings.Normalize validates the URL
	remote.Server = u.Hostname()
	if u.Port() != "" {
		port, _ := strconv.ParseUint(u.Port(), 10, 16)
		remote.ServerPort = uint16(port)
	}
	if _, err := netip.ParseAddr(remote.Server); err != nil {
		remote.DomainResolver = &option.DomainResolveOptions{Server: "local"}
	}
	servers := []option.DNSServerOptions{
		{
			Type: C.DNSTypeHTTPS,
			Tag:  "remote",
			Options: &option.RemoteHTTPSDNSServerOptions{
				RemoteTLSDNSServerOptions: option.RemoteTLSDNSServerOptions{RemoteDNSServerOptions: remote},
				Path:                      u.EscapedPath(),
			},
		},
	}
	if remote.DomainResolver != nil {
		servers = append(servers, option.DNSServerOptions{
			Type: C.DNSTypeLocal,
			Tag:  "local",
			Options: &option.LocalDNSServerOptions{
				PreferGo: true,
				RawLocalDNSServerOptions: option.RawLocalDNSServerOptions{
					DialerOptions: option.DialerOptions{Detour: "direct"},
				},
			},
		})
	}
	return servers
}

func listenAddr(s domain.Settings) netip.Addr {
	if s.AllowLAN == "on" {
		return netip.IPv4Unspecified()
	}
	return netip.MustParseAddr("127.0.0.1")
}
