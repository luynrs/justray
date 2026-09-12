package domain

import (
	"fmt"
	"net/netip"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

const (
	DefaultPort     = 10808
	DefaultDNS      = "8.8.8.8"
	DefaultLogLevel = "error"
	DefaultTunMTU   = 9000
	DefaultTunStack = "gvisor"
	DefaultRefresh  = 6
	DefaultProbeURL = "http://www.gstatic.com/generate_204"
	TunInterface    = "justray"
)

const (
	ProxyAll  = "proxy all"
	DirectAll = "direct all"
)

var (
	LogLevels  = []string{"error", "warn", "info", "debug"}
	TunStacks  = []string{"gvisor", "system", "mixed"}
	IPVersions = []string{"auto", "ipv4", "ipv6"}
	Modes      = []string{ProxyAll, DirectAll}
	Toggle     = []string{"on", "off"}
)

type Settings struct {
	General    `json:"general,omitempty"`
	Connection `json:"connection,omitempty"`
	Routing    `json:"routing,omitempty"`
}

type General struct {
	Autostart    string `json:"-"`                       // on/off, kept by the OS
	RefreshEvery int    `json:"refresh_hours,omitempty"` // 0 = never
	Emoji        string `json:"emoji,omitempty"`
	ForceTTY     string `json:"force_tty,omitempty"` // on/off
	LogLevel     string `json:"log_level,omitempty"`
	ProbeURL     string `json:"probe_url,omitempty"`
}

type Connection struct {
	Port      int    `json:"port,omitempty"`
	AllowLAN  string `json:"allow_lan,omitempty"` // on/off
	TunStack  string `json:"stack,omitempty"`
	TunMTU    int    `json:"mtu,omitempty"`
	DNS       string `json:"dns,omitempty"`
	DNSHijack string `json:"dns_hijack,omitempty"` // on/off, empty = on
	IPVersion string `json:"ip_version,omitempty"`
}

type Routing struct {
	Mode        string   `json:"mode,omitempty"`         // proxy all/direct all, empty = proxy all
	BypassLocal string   `json:"bypass_local,omitempty"` // on/off, empty = on
	TunStrict   string   `json:"strict_route,omitempty"` // on/off, empty = on
	BlockQUIC   string   `json:"block_quic,omitempty"`   // on/off, empty = off
	Direct      []string `json:"direct,omitempty"`       // forced direct
	Proxy       []string `json:"proxy,omitempty"`        // forced through proxy
	Block       []string `json:"block,omitempty"`        // rejected
}

func (s Settings) IPv4() bool            { return s.IPVersion != "ipv6" }
func (s Settings) IPv6() bool            { return s.IPVersion != "ipv4" }
func (s Settings) Equal(o Settings) bool { return reflect.DeepEqual(s, o) }

// Normalize fills defaults and validates
func (s Settings) Normalize() (Settings, error) {
	checks := []error{
		num("port", &s.Port, DefaultPort, 1, 65535),
		num("mtu", &s.TunMTU, DefaultTunMTU, 576, 65535),
		num("refresh interval", &s.RefreshEvery, 0, 0, 24*30),
		one("log level", &s.LogLevel, DefaultLogLevel, LogLevels),
		one("stack", &s.TunStack, DefaultTunStack, TunStacks),
		one("ip version", &s.IPVersion, "auto", IPVersions),
		one("mode", &s.Mode, ProxyAll, Modes),
		one("strict route", &s.TunStrict, "on", Toggle),
		one("dns hijack", &s.DNSHijack, "on", Toggle),
		one("allow lan", &s.AllowLAN, "off", Toggle),
		one("direct lan", &s.BypassLocal, "on", Toggle),
		one("block quic", &s.BlockQUIC, "off", Toggle),
		one("force tty", &s.ForceTTY, "off", Toggle),
		one("autostart", &s.Autostart, "off", Toggle),
		one("emoji", &s.Emoji, "off", Toggle),
		text("dns", &s.DNS, DefaultDNS, "an IP address or an HTTPS URL", isDNS),
		text("probe url", &s.ProbeURL, DefaultProbeURL, "a url", isURL),
		canon(&s.Direct),
		canon(&s.Proxy),
		canon(&s.Block),
		disjoint(s.Direct, s.Proxy),
	}
	for _, err := range checks {
		if err != nil {
			return s, err
		}
	}
	return s, nil
}

func num(name string, v *int, def, lo, hi int) error {
	if *v == 0 {
		*v = def
	}
	if *v < lo || *v > hi {
		return fmt.Errorf("%s %d is out of range", name, *v)
	}
	return nil
}

func one(name string, v *string, def string, allowed []string) error {
	if *v == "" {
		*v = def
	}
	if !slices.Contains(allowed, *v) {
		return fmt.Errorf("%s %q is not one of %s", name, *v, strings.Join(allowed, ", "))
	}
	return nil
}

func text(name string, v *string, def, want string, ok func(string) bool) error {
	if *v = strings.TrimSpace(*v); *v == "" {
		*v = def
	}
	if !ok(*v) {
		return fmt.Errorf("%s %q is not %s", name, *v, want)
	}
	return nil
}

func canon(list *[]string) error {
	out := make([]string, 0, len(*list))
	for _, raw := range *list {
		rule, err := ParseRule(raw)
		if err != nil {
			return err
		}
		if !slices.Contains(out, rule) {
			out = append(out, rule)
		}
	}
	*list = out
	return nil
}

func disjoint(a, b []string) error {
	for _, x := range a {
		if slices.Contains(b, x) {
			return fmt.Errorf("%q cannot be in both direct and proxy", x)
		}
	}
	return nil
}

func isAddr(v string) bool {
	_, err := netip.ParseAddr(v)
	return err == nil
}

func isURL(v string) bool {
	u, err := url.Parse(v)
	return err == nil && u.Host != "" && (u.Scheme == "http" || u.Scheme == "https")
}

func isDNS(v string) bool {
	if isAddr(v) {
		return true
	}
	u, err := url.Parse(v)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || strings.HasSuffix(u.Host, ":") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	if u.Port() != "" {
		port, parseErr := strconv.ParseUint(u.Port(), 10, 16)
		return parseErr == nil && port != 0
	}
	return true
}

// ParseRule canonicalises a network, domain or program rule
func ParseRule(raw string) (string, error) {
	rule := strings.TrimSpace(raw)
	if p, err := parsePrefix(rule); err == nil {
		return p.String(), nil
	}
	rule, star := strings.CutPrefix(rule, "*.")
	rule, keyword := strings.CutSuffix(rule, ".*")
	rule = strings.Trim(rule, ".")
	if rule == "" || strings.Contains(rule, "://") || strings.ContainsAny(rule, "\t\n\r@?#*") {
		return "", fmt.Errorf("%q is not a network, a domain or a program", raw)
	}
	switch {
	case keyword:
		return rule + ".*", nil
	case star:
		return "*." + rule, nil
	}
	return rule, nil
}

// SplitRules splits entries into cidrs, domains, keywords, names and paths
func SplitRules(list []string) (cidrs, domains, keywords, names, paths []string) {
	for _, rule := range list {
		lower := strings.ToLower(rule)
		switch {
		case isPrefix(rule):
			cidrs = append(cidrs, rule)
		case strings.HasSuffix(lower, ".*"):
			keywords = append(keywords, strings.TrimSuffix(lower, "*"))
		case strings.HasPrefix(lower, "*."):
			domains = append(domains, lower[1:])
		case strings.ContainsAny(rule, `/\`):
			paths = append(paths, rule)
		case strings.Contains(rule, " "), strings.HasSuffix(lower, ".exe"):
			names = append(names, rule)
		case strings.Contains(rule, "."):
			domains = append(domains, lower)
		default:
			names = append(names, rule)
			domains = append(domains, lower)
		}
	}
	return cidrs, domains, keywords, names, paths
}

func isPrefix(rule string) bool {
	_, err := netip.ParsePrefix(rule)
	return err == nil
}

// parsePrefix accepts a CIDR or a bare address
func parsePrefix(raw string) (netip.Prefix, error) {
	raw = strings.TrimSpace(raw)
	if p, err := netip.ParsePrefix(raw); err == nil {
		return p.Masked(), nil
	}
	a, err := netip.ParseAddr(raw)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("%q is not an ip or a cidr", raw)
	}
	return netip.PrefixFrom(a, a.BitLen()), nil
}
