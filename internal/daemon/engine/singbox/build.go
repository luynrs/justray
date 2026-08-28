package singbox

import (
	"net/netip"
	"strconv"
	"strings"
	"sync"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/json/badoption"

	"github.com/luynrs/justray/internal/daemon/engine/singbox/outbound"
	"github.com/luynrs/justray/internal/daemon/engine/singbox/resolvers"
	"github.com/luynrs/justray/internal/shared/domain"
)

const Tag = "proxy"

func Build(n domain.Node, s domain.Settings, logPath string, tun bool) (*option.Options, error) {
	ep, obs, err := Proxy(n, s)
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
			Servers: []option.DNSServerOptions{
				{Type: C.DNSTypeTCP, Tag: "remote", Options: &option.RemoteDNSServerOptions{
					RawLocalDNSServerOptions: option.RawLocalDNSServerOptions{
						DialerOptions: option.DialerOptions{Detour: strings.TrimPrefix(final(s), "direct")}, // a detour to a bare direct outbound is rejected by sing-box
					},
					DNSServerAddressOptions: option.DNSServerAddressOptions{Server: s.DNS},
				}},
			}}},
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

func Proxy(n domain.Node, s domain.Settings) (*option.Endpoint, []option.Outbound, error) {
	n, err := resolved(n, s)
	if err != nil {
		return nil, nil, err
	}
	return outbound.New(n, Tag)
}

func ProbeTag(i int) string { return "p" + strconv.Itoa(i) }

func ProbeConfig(nodes []domain.Node, s domain.Settings, logPath string) *option.Options {
	opts := &option.Options{
		Log:   &option.LogOptions{Level: s.LogLevel, Output: logPath},
		Route: &option.RouteOptions{AutoDetectInterface: true},
	}
	resolvedNodes := make([]domain.Node, len(nodes))
	var wg sync.WaitGroup
	for i, n := range nodes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if r, err := resolved(n, s); err == nil {
				n = r
			}
			resolvedNodes[i] = n
		}()
	}
	wg.Wait()

	for i, n := range resolvedNodes {
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
