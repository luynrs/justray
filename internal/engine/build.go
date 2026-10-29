package engine

import (
	"net/netip"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json/badoption"

	"github.com/luynrs/justray/internal/domain"
)

var dnsStrategy = map[string]option.DomainStrategy{
	"ipv4":  option.DomainStrategy(C.DomainStrategyIPv4Only),
	"ipv6":  option.DomainStrategy(C.DomainStrategyPreferIPv6),
	"mixed": option.DomainStrategy(C.DomainStrategyPreferIPv4),
}

func build(n domain.Node, s domain.Settings, logPath string, tun bool) *option.Options {
	dnsDetour := detour(s)
	opts := &option.Options{
		Log: &option.LogOptions{Level: s.LogLevel, Output: logPath, Timestamp: true},
		Inbounds: []option.Inbound{
			{Type: C.TypeMixed, Tag: "mixed-in", Options: &option.HTTPMixedInboundOptions{
				ListenOptions: option.ListenOptions{
					Listen:     new(badoption.Addr(listenAddr(s))),
					ListenPort: uint16(s.Port),
				},
			}},
		},
		Outbounds: []option.Outbound{
			{Type: C.TypeDirect, Tag: "direct", Options: &option.DirectOutboundOptions{}},
			{Type: "justray-node", Tag: "proxy", Options: &n},
		},
		DNS: &option.DNSOptions{RawDNSOptions: option.RawDNSOptions{
			ReverseMapping:   true,
			DNSClientOptions: option.DNSClientOptions{Strategy: dnsStrategy[s.IPVersion]},
			Servers:          dnsServers(s.DNS, dnsDetour, "remote"),
			Final:            "remote",
		}},
		Route: &option.RouteOptions{
			Final:                 final(s),
			AutoDetectInterface:   true,
			DefaultDomainResolver: &option.DomainResolveOptions{Server: "remote", Strategy: option.DomainStrategy(C.DomainStrategyPreferIPv4)},
			Rules:                 rules(s),
			RuleSet:               ruleSets(s),
		},
	}
	if len(opts.Route.RuleSet) > 0 {
		opts.Experimental = &option.ExperimentalOptions{CacheFile: &option.CacheFileOptions{
			Enabled: true, Path: filepath.Join(filepath.Dir(filepath.Dir(logPath)), "cache.db"),
		}}
	}
	if dnsDetour != "" {
		opts.DNS.Servers = append(opts.DNS.Servers, dnsServers(s.DNS, "", "node")...)
		opts.Route.DefaultDomainResolver.Server = "node"
	}
	if tun {
		opts.Inbounds = append(opts.Inbounds, tunInbound(s))
	}
	return opts
}

func probeConfig(s domain.Settings, logPath string) *option.Options {
	return &option.Options{
		Log: &option.LogOptions{Level: s.LogLevel, Output: logPath, Timestamp: true},
		Route: &option.RouteOptions{
			AutoDetectInterface:   true,
			DefaultDomainResolver: &option.DomainResolveOptions{Server: "remote", Strategy: option.DomainStrategy(C.DomainStrategyPreferIPv4)},
		},
		Outbounds: []option.Outbound{{Type: C.TypeDirect, Tag: "direct", Options: &option.DirectOutboundOptions{}}},
		DNS: &option.DNSOptions{RawDNSOptions: option.RawDNSOptions{
			DNSClientOptions: option.DNSClientOptions{Strategy: dnsStrategy[s.IPVersion]},
			Servers:          dnsServers(s.DNS, "", "remote"),
			Final:            "remote",
		}},
	}
}

func detour(s domain.Settings) string {
	if final(s) != "proxy" {
		return ""
	}
	host := s.DNS
	if u, err := url.Parse(s.DNS); err == nil && u.Hostname() != "" {
		host = u.Hostname()
	} else if ap, err := netip.ParseAddrPort(s.DNS); err == nil {
		host = ap.Addr().String()
	}
	if strings.EqualFold(strings.TrimSuffix(host, "."), "localhost") {
		return ""
	}
	if addr, err := netip.ParseAddr(host); err == nil && (addr.IsLoopback() || addr.IsLinkLocalUnicast() || (s.BypassLocal == "on" && addr.IsPrivate())) {
		return ""
	}
	return "proxy"
}

func dnsServers(address, detourTag, tag string) []option.DNSServerOptions {
	remote := option.RemoteDNSServerOptions{
		RawLocalDNSServerOptions: option.RawLocalDNSServerOptions{
			DialerOptions: option.DialerOptions{Detour: detourTag},
		},
		DNSServerAddressOptions: option.DNSServerAddressOptions{Server: address},
	}
	transportType := C.DNSTypeUDP
	if detourTag != "" {
		transportType = C.DNSTypeTCP
	}
	u, _ := url.Parse(address) // Settings.Normalize validates the URL
	if u != nil && u.Hostname() != "" {
		transportType = C.DNSTypeHTTPS
		remote.Server = u.Hostname()
		if u.Port() != "" {
			port, _ := strconv.ParseUint(u.Port(), 10, 16)
			remote.ServerPort = uint16(port)
		}
	} else if endpoint, err := netip.ParseAddrPort(address); err == nil {
		remote.Server = endpoint.Addr().String()
		remote.ServerPort = endpoint.Port()
	}
	if transportType != C.DNSTypeHTTPS {
		return []option.DNSServerOptions{{Type: transportType, Tag: tag, Options: &remote}}
	}
	if _, err := netip.ParseAddr(remote.Server); err != nil {
		remote.DomainResolver = &option.DomainResolveOptions{Server: tag + "-bootstrap", Strategy: option.DomainStrategy(C.DomainStrategyPreferIPv4)}
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
