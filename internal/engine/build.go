package engine

import (
	"context"
	"net/netip"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"sync"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/json/badoption"

	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/engine/outbound"
	"github.com/luynrs/justray/internal/engine/resolvers"
)

const (
	Tag           = "proxy"
	maxProbeNodes = 512
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

	resolverIPs := resolvers.Get()
	resolverCIDRs := make([]string, 0, len(resolverIPs))
	for _, p := range resolverIPs {
		resolverCIDRs = append(resolverCIDRs, p.String())
	}

	opts := &option.Options{
		Log: &option.LogOptions{Level: s.LogLevel, Output: logPath},
		Inbounds: []option.Inbound{
			{Type: C.TypeMixed, Tag: "mixed-in", Options: &option.HTTPMixedInboundOptions{
				ListenOptions: option.ListenOptions{
					Listen:     common.Ptr(badoption.Addr(netip.MustParseAddr("127.0.0.1"))),
					ListenPort: uint16(s.Port),
				},
			}},
		},
		Outbounds: []option.Outbound{
			{Type: C.TypeDirect, Tag: "direct", Options: &option.DirectOutboundOptions{}},
		},
		DNS: &option.DNSOptions{RawDNSOptions: option.RawDNSOptions{
			DNSClientOptions: option.DNSClientOptions{Strategy: dnsStrategy[s.IPVersion]},
			Servers:          []option.DNSServerOptions{dnsServer(s)},
		}},
		Route: &option.RouteOptions{
			Final:               final(s),
			AutoDetectInterface: true,
			Rules:               rules(s, resolverCIDRs),
		},
	}
	attach(opts, ep, obs)
	if tun {
		opts.Inbounds = append(opts.Inbounds, TunInbound(s, resolverIPs))
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

func probeWorkers(n int) int { return min(n, max(runtime.NumCPU()*2, 4)) }

func ProbeConfig(ctx context.Context, nodes []domain.Node, s domain.Settings, logPath string) *option.Options {
	opts := &option.Options{
		Log:   &option.LogOptions{Level: s.LogLevel, Output: logPath},
		Route: &option.RouteOptions{AutoDetectInterface: true},
	}
	uniqueHosts := map[string]string{}
	for _, n := range nodes {
		if _, err := netip.ParseAddr(n.Server); err != nil && n.Server != "" {
			uniqueHosts[n.Server] = ""
		}
	}
	hosts := make([]string, 0, len(uniqueHosts))
	for host := range uniqueHosts {
		hosts = append(hosts, host)
	}
	var mu sync.Mutex
	sem := make(chan struct{}, probeWorkers(len(hosts)))
	var wg sync.WaitGroup
	for _, host := range hosts {
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			dummy := domain.Node{Server: host}
			if r, err := resolved(ctx, dummy, s); err == nil {
				mu.Lock()
				uniqueHosts[host] = r.Server
				mu.Unlock()
			}
		})
	}
	wg.Wait()

	for i, n := range nodes {
		if ip, ok := uniqueHosts[n.Server]; ok && ip != "" {
			n = withServerIP(n, ip)
		}
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

func dnsServer(s domain.Settings) option.DNSServerOptions {
	detour := ""
	if final(s) == Tag {
		detour = Tag
	}
	remote := option.RemoteDNSServerOptions{
		RawLocalDNSServerOptions: option.RawLocalDNSServerOptions{
			DialerOptions: option.DialerOptions{Detour: detour},
		},
		DNSServerAddressOptions: option.DNSServerAddressOptions{Server: s.DNS},
	}
	if !strings.HasPrefix(s.DNS, "https://") {
		return option.DNSServerOptions{Type: C.DNSTypeTCP, Tag: "remote", Options: &remote}
	}

	u, _ := url.Parse(s.DNS) // Settings.Normalize validates the URL
	remote.Server = u.Hostname()
	if u.Port() != "" {
		port, _ := strconv.ParseUint(u.Port(), 10, 16)
		remote.ServerPort = uint16(port)
	}
	return option.DNSServerOptions{Type: C.DNSTypeHTTPS, Tag: "remote", Options: &option.RemoteHTTPSDNSServerOptions{
		RemoteTLSDNSServerOptions: option.RemoteTLSDNSServerOptions{RemoteDNSServerOptions: remote},
		Path:                      u.EscapedPath(),
	}}
}
