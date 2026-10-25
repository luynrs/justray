package engine

import (
	"net/netip"
	"runtime"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"

	"github.com/luynrs/justray/internal/domain"
)

var (
	reject = option.RuleAction{
		Action:        C.RuleActionTypeReject,
		RejectOptions: option.RejectActionOptions{Method: C.RuleActionRejectMethodDefault},
	}
	toDirect = option.RuleAction{Action: C.RuleActionTypeRoute, RouteOptions: option.RouteActionOptions{Outbound: "direct"}}
	toProxy  = option.RuleAction{Action: C.RuleActionTypeRoute, RouteOptions: option.RouteActionOptions{Outbound: "proxy"}}
)

func match(list []string, action option.RuleAction) []option.Rule {
	cidrs, domains, keywords, names, paths := domain.SplitRules(list)

	var out []option.Rule
	for _, m := range []option.RawDefaultRule{
		{ProcessName: names}, {ProcessPath: paths},
		{IPCIDR: cidrs}, {DomainSuffix: domains}, {DomainKeyword: keywords},
	} {
		if len(m.ProcessName)+len(m.ProcessPath)+len(m.IPCIDR)+len(m.DomainSuffix)+len(m.DomainKeyword) == 0 {
			continue
		}
		out = append(out, option.Rule{Type: C.RuleTypeDefault, DefaultOptions: option.DefaultRule{
			RawDefaultRule: m,
			RuleAction:     action,
		}})
	}
	return out
}

func rules(s domain.Settings) []option.Rule {
	var out []option.Rule
	if s.DNSHijack == "on" {
		out = append(out, option.Rule{Type: C.RuleTypeDefault, DefaultOptions: option.DefaultRule{
			RawDefaultRule: option.RawDefaultRule{Inbound: []string{"tun-in"}, Port: []uint16{53}},
			RuleAction:     option.RuleAction{Action: C.RuleActionTypeHijackDNS},
		}})
	}
	// Deprecated: wlbe in 1.7.1 with new port settings
	out = append(out, option.Rule{Type: C.RuleTypeDefault, DefaultOptions: option.DefaultRule{
		RawDefaultRule: option.RawDefaultRule{Inbound: []string{"mixed-in"}},
		RuleAction:     toProxy,
	}})
	out = append(out, option.Rule{Type: C.RuleTypeDefault, DefaultOptions: option.DefaultRule{RuleAction: option.RuleAction{Action: C.RuleActionTypeSniff}}})
	if s.BlockQUIC == "on" {
		out = append(out, option.Rule{Type: C.RuleTypeDefault, DefaultOptions: option.DefaultRule{
			RawDefaultRule: option.RawDefaultRule{Network: []string{"udp"}, Port: []uint16{443}},
			RuleAction:     reject,
		}})
	}

	// Block > ex. Direct/Proxy > Mode
	out = append(out, match(s.Block, reject)...)
	out = append(out, match(s.Direct, toDirect)...)
	out = append(out, match(s.Proxy, toProxy)...)

	if s.BypassLocal == "on" {
		out = append(out, option.Rule{Type: C.RuleTypeDefault, DefaultOptions: option.DefaultRule{
			RawDefaultRule: option.RawDefaultRule{IPIsPrivate: true},
			RuleAction:     toDirect,
		}})
	}
	return out
}

func tunInbound(s domain.Settings) option.Inbound {
	interfaceName := domain.Interface
	if runtime.GOOS == "darwin" {
		interfaceName = "" // macOS assigns utun names
	}

	tunOpts := &option.TunInboundOptions{
		InterfaceName: interfaceName,
		MTU:           uint32(s.TunMTU),
		Stack:         s.TunStack,
		Address:       []netip.Prefix{netip.MustParsePrefix("172.19.0.1/30")},
		AutoRoute:     true,
		StrictRoute:   s.TunStrict == "on",
	}
	if s.IPVersion != "ipv4" {
		tunOpts.Address = append(tunOpts.Address, netip.MustParsePrefix("fdfe:dcba:9876::1/126"))
	}
	return option.Inbound{Type: C.TypeTun, Tag: "tun-in", Options: tunOpts}
}

func final(s domain.Settings) string {
	if s.Mode == "direct all" {
		return "direct"
	}
	return "proxy"
}
