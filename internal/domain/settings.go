package domain

import (
	"fmt"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

const (
	DefaultPort      = 10808
	DefaultDNS       = "8.8.8.8"
	DefaultLogLevel  = "error"
	DefaultTunMTU    = 9000
	DefaultTunStack  = "gvisor"
	DefaultIPVersion = "ipv4"
	DefaultRefresh   = 6
	DefaultProbeURL  = "http://www.gstatic.com/generate_204"
	Interface        = "justray"
)

var (
	LogLevels  = []string{"error", "warn", "info", "debug"}
	TunStacks  = []string{"gvisor", "system", "mixed"}
	IPVersions = []string{"ipv4", "ipv6", "mixed"}
	Modes      = []string{"proxy all", "direct all"}
	ProxyTypes = []string{"local", "system"}
	Toggle     = []string{"on", "off"}
)

type Settings struct {
	General    `json:"general"`
	Connection `json:"connection"`
	Routing    `json:"routing"`
}

type General struct {
	Autostart    string  `json:"autostart,omitempty"` // on/off, kept by the OS
	RefreshEvery int     `json:"refresh_hours"`       // 0 = never
	Emoji        string  `json:"emoji"`
	ForceTTY     string  `json:"force_tty"` // on/off
	LogLevel     string  `json:"log_level"`
	ProbeURL     string  `json:"probe_url"`
	Details      Details `json:"details"`
}

type Details struct {
	Address   string `json:"address"`
	Transport string `json:"transport"`
	Protocol  string `json:"protocol"`
}

type Connection struct {
	ProxyType string `json:"proxy_type"`
	Port      int    `json:"port"`
	AllowLAN  string `json:"allow_lan"` // on/off
	IPVersion string `json:"ip_version"`
	TunStack  string `json:"stack"`
	TunMTU    int    `json:"mtu"`
	DNS       string `json:"dns"`
	DNSHijack string `json:"dns_hijack"` // on/off, empty = on
}

type Routing struct {
	Mode        string   `json:"mode"`         // proxy all/direct all, empty = proxy all
	BypassLocal string   `json:"bypass_local"` // on/off, empty = on
	TunStrict   string   `json:"strict_route"` // on/off, empty = on
	BlockQUIC   string   `json:"block_quic"`   // on/off, empty = off
	Direct      []string `json:"direct"`       // forced direct
	Proxy       []string `json:"proxy"`        // forced proxy
	Block       []string `json:"block"`        // reject
}

func (s Settings) Equal(o Settings) bool {
	return s.General == o.General && s.Connection == o.Connection &&
		s.Mode == o.Mode && s.BypassLocal == o.BypassLocal && s.TunStrict == o.TunStrict &&
		s.BlockQUIC == o.BlockQUIC &&
		slices.Equal(s.Direct, o.Direct) && slices.Equal(s.Proxy, o.Proxy) && slices.Equal(s.Block, o.Block)
}

func (s Settings) Clone() Settings {
	s.Direct = slices.Clone(s.Direct)
	s.Proxy = slices.Clone(s.Proxy)
	s.Block = slices.Clone(s.Block)
	return s
}

// Normalize fills defaults and validates
func (s Settings) Normalize() (Settings, error) {
	s.ProxyType = strings.ToLower(s.ProxyType)
	checks := []error{
		num("port", &s.Port, DefaultPort, 1, 65535),
		num("mtu", &s.TunMTU, DefaultTunMTU, 1280, 65535),
		num("refresh interval", &s.RefreshEvery, 0, 0, 24*30),
		one("log level", &s.LogLevel, DefaultLogLevel, LogLevels),
		one("stack", &s.TunStack, DefaultTunStack, TunStacks),
		one("ip version", &s.IPVersion, DefaultIPVersion, IPVersions),
		one("proxy type", &s.ProxyType, "local", ProxyTypes),
		one("mode", &s.Mode, "proxy all", Modes),
		one("strict route", &s.TunStrict, "on", Toggle),
		one("dns hijack", &s.DNSHijack, "on", Toggle),
		one("allow lan", &s.AllowLAN, "off", Toggle),
		one("direct lan", &s.BypassLocal, "on", Toggle),
		one("block quic", &s.BlockQUIC, "off", Toggle),
		one("force tty", &s.ForceTTY, "off", Toggle),
		one("emoji", &s.Emoji, "off", Toggle),
		one("node address", &s.Details.Address, "on", Toggle),
		one("node transport", &s.Details.Transport, "off", Toggle),
		one("node protocol", &s.Details.Protocol, "on", Toggle),
		text("dns", &s.DNS, DefaultDNS, "IP address or HTTPS URL", isDNS),
		text("probe url", &s.ProbeURL, DefaultProbeURL, "HTTP(S) URL", isURL),
		canon(&s.Direct),
		canon(&s.Proxy),
		canon(&s.Block),
		disjoint(s.Direct, s.Proxy),
	}
	if s.Autostart != "" {
		checks = append(checks, one("autostart", &s.Autostart, "off", Toggle))
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
		return fmt.Errorf("%s %d out of range (%d-%d)", name, *v, lo, hi)
	}
	return nil
}

func one(name string, v *string, def string, allowed []string) error {
	if *v == "" {
		*v = def
	}
	if !slices.Contains(allowed, *v) {
		return fmt.Errorf("invalid %s %q (allowed: %s)", name, *v, strings.Join(allowed, ", "))
	}
	return nil
}

func text(name string, v *string, def, want string, ok func(string) bool) error {
	if *v = strings.TrimSpace(*v); *v == "" {
		*v = def
	}
	if !ok(*v) {
		return fmt.Errorf("invalid %s %q (expected: %s)", name, *v, want)
	}
	return nil
}

func canon(list *[]string) error {
	out := make([]string, 0, len(*list))
	seen := make(map[string]struct{}, len(*list))
	for _, raw := range *list {
		rule, err := ParseRule(raw)
		if err != nil {
			return err
		}
		if _, exists := seen[rule]; !exists {
			out = append(out, rule)
			seen[rule] = struct{}{}
		}
	}
	*list = out
	return nil
}

func disjoint(a, b []string) error {
	if len(a) == 0 || len(b) == 0 {
		return nil
	}
	rules := make(map[string]struct{}, len(b))
	for _, rule := range b {
		rules[rule] = struct{}{}
	}
	for _, rule := range a {
		if _, exists := rules[rule]; exists {
			return fmt.Errorf("rule %q is defined in both direct and proxy", rule)
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
	if ap, err := netip.ParseAddrPort(v); err == nil {
		return ap.Port() != 0
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
	if kind, name, ok := strings.Cut(strings.ToLower(rule), ":"); ok && (kind == "geoip" || kind == "geosite") {
		if name == "" {
			return "", fmt.Errorf("invalid routing rule %q", raw)
		}
		for index, char := range name {
			if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' {
				continue
			}
			if index > 0 && strings.ContainsRune("-_!@", char) {
				continue
			}
			return "", fmt.Errorf("invalid routing rule %q", raw)
		}
		return kind + ":" + name, nil
	}
	if prefix, err := parsePrefix(rule); err == nil {
		return prefix.String(), nil
	}
	if host, _, found := strings.Cut(strings.ReplaceAll(rule, `\`, "/"), "/"); found && isAddr(host) {
		return "", fmt.Errorf("invalid routing rule %q", raw)
	}
	hasDrivePrefix := len(rule) >= 3 &&
		((rule[0] >= 'a' && rule[0] <= 'z') || (rule[0] >= 'A' && rule[0] <= 'Z')) &&
		rule[1] == ':' && (rule[2] == '\\' || rule[2] == '/')
	if strings.Contains(rule, ":") && !hasDrivePrefix {
		return "", fmt.Errorf("invalid routing rule %q", raw)
	}
	leadingWildcard, trailingWildcard := strings.HasPrefix(rule, "*"), strings.HasSuffix(rule, "*")
	rule = strings.Trim(rule, "*.")
	if rule == "" || strings.Contains(rule, "://") || strings.ContainsAny(rule, "\t\n\r@?#*") {
		return "", fmt.Errorf("invalid routing rule %q", raw)
	}
	if !strings.ContainsAny(rule, `/\`) && (leadingWildcard || trailingWildcard ||
		(!strings.Contains(rule, " ") && !strings.HasSuffix(strings.ToLower(rule), ".exe") && strings.Contains(rule, "."))) {
		rule = strings.ToLower(rule)
	}
	switch {
	case leadingWildcard && trailingWildcard:
		return "*" + rule + "*", nil
	case trailingWildcard:
		return rule + ".*", nil
	case leadingWildcard:
		return "*." + rule, nil
	}
	return rule, nil
}

// SplitRules splits entries into cidrs, domains, keywords, names and paths
func SplitRules(list []string) (cidrs, domains, keywords, names, paths, sets []string) {
	for _, raw := range list {
		rule := strings.TrimSpace(raw)
		if rule == "" {
			continue
		}
		lower := strings.ToLower(rule)
		switch {
		case strings.HasPrefix(lower, "geoip:"), strings.HasPrefix(lower, "geosite:"):
			sets = append(sets, strings.Replace(lower, ":", "-", 1))
		case isPrefix(rule):
			cidrs = append(cidrs, rule)
		case strings.ContainsAny(rule, `/\`):
			paths = append(paths, rule)
		case strings.HasSuffix(lower, "*"):
			if kw := strings.Trim(lower, "*."); kw != "" {
				keywords = append(keywords, kw)
			}
		case strings.HasPrefix(lower, "*."):
			domains = append(domains, lower[1:])
		case strings.Contains(rule, " "), strings.HasSuffix(lower, ".exe"):
			names = append(names, rule)
		case strings.Contains(rule, "."):
			domains = append(domains, lower)
		default:
			names = append(names, rule)
		}
	}
	return cidrs, domains, keywords, names, paths, sets
}

func isPrefix(rule string) bool {
	_, err := netip.ParsePrefix(rule)
	return err == nil
}

// parsePrefix accepts a CIDR or a bare address
func parsePrefix(raw string) (netip.Prefix, error) {
	if p, err := netip.ParsePrefix(raw); err == nil {
		return p.Masked(), nil
	}
	a, err := netip.ParseAddr(raw)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("invalid IP or CIDR %q", raw)
	}
	return netip.PrefixFrom(a, a.BitLen()), nil
}
