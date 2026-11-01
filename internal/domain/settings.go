package domain

import (
	"cmp"
	"fmt"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
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
	*v = cmp.Or(*v, def)
	if *v < lo || *v > hi {
		return fmt.Errorf("%s out of range (%d-%d): %d", name, lo, hi, *v)
	}
	return nil
}

func one(name string, v *string, def string, allowed []string) error {
	*v = cmp.Or(*v, def)
	if !slices.Contains(allowed, *v) {
		return fmt.Errorf("invalid %s (allowed: %s): %s", name, strings.Join(allowed, ", "), *v)
	}
	return nil
}

func text(name string, v *string, def, want string, ok func(string) bool) error {
	*v = cmp.Or(strings.TrimSpace(*v), def)
	if !ok(*v) {
		return fmt.Errorf("invalid %s: expected %s", name, want)
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
			return fmt.Errorf("rule defined in both direct and proxy: %s", rule)
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
	return err == nil && u.Hostname() != "" && (u.Scheme == "http" || u.Scheme == "https")
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

type routingRuleKind uint8

const (
	ruleCIDR routingRuleKind = iota
	ruleDomain
	ruleKeyword
	ruleProcess
	rulePath
	ruleSet
)

func ParseRule(raw string) (string, error) {
	rule := strings.TrimSpace(raw)
	if rule == "" || !utf8.ValidString(rule) || strings.IndexFunc(rule, unicode.IsControl) >= 0 || strings.Contains(rule, "://") {
		return "", fmt.Errorf("invalid routing rule: %s", raw)
	}
	kind, rule := classify(rule)
	switch kind {
	case ruleSet:
		_, name, _ := strings.Cut(rule, ":")
		if name == "" {
			return "", fmt.Errorf("invalid routing rule: %s", raw)
		}
		for index, char := range name {
			if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || index > 0 && strings.ContainsRune("-_!@", char) {
				continue
			}
			return "", fmt.Errorf("invalid routing rule: %s", raw)
		}
	case rulePath:
		if separator := strings.IndexAny(rule, `/\`); separator > 0 && isAddr(rule[:separator]) {
			return "", fmt.Errorf("invalid routing rule: %s", raw)
		}
	case ruleDomain, ruleKeyword:
		leading := strings.HasPrefix(rule, "*")
		body := rule
		if strings.Contains(rule, "*") {
			body = strings.Trim(strings.TrimSuffix(strings.TrimPrefix(rule, "*"), "*"), ".")
			if body == "" || strings.ContainsAny(body, "* :@?#") {
				return "", fmt.Errorf("invalid routing rule: %s", raw)
			}
		}
		if kind == ruleDomain {
			domain, ok := normalize(body)
			if !ok {
				return "", fmt.Errorf("invalid routing rule: %s", raw)
			}
			if leading {
				return "*." + domain, nil
			}
			return domain, nil
		}
		return "*" + strings.ToLower(body) + "*", nil
	case ruleProcess:
		if strings.Contains(rule, ":") || strings.Trim(rule, ".") == "" {
			return "", fmt.Errorf("invalid routing rule: %s", raw)
		}
	}
	return rule, nil
}

func SplitRules(list []string) (cidrs, domains, keywords, names, paths, sets []string) {
	for _, rule := range list {
		kind, rule := classify(rule)
		switch kind {
		case ruleCIDR:
			cidrs = append(cidrs, rule)
		case rulePath:
			paths = append(paths, rule)
		case ruleSet:
			sets = append(sets, strings.Replace(rule, ":", "-", 1))
		case ruleKeyword:
			keywords = append(keywords, strings.Trim(rule, "*."))
		case ruleDomain:
			domains = append(domains, strings.TrimPrefix(rule, "*"))
		case ruleProcess:
			names = append(names, rule)
		}
	}
	return cidrs, domains, keywords, names, paths, sets
}

func classify(rule string) (routingRuleKind, string) {
	if kind, _, found := strings.Cut(rule, ":"); found && (strings.EqualFold(kind, "geoip") || strings.EqualFold(kind, "geosite")) {
		return ruleSet, strings.ToLower(rule)
	}
	if prefix, ok := prefix(rule); ok {
		return ruleCIDR, prefix.String()
	}
	switch {
	case strings.ContainsAny(rule, `/\`):
		return rulePath, rule
	case strings.Contains(rule, "*"):
		if strings.HasSuffix(rule, "*") {
			return ruleKeyword, rule
		}
		return ruleDomain, rule
	case strings.HasPrefix(rule, "."), strings.Contains(rule, " "), len(rule) >= 4 && strings.EqualFold(rule[len(rule)-4:], ".exe"):
		return ruleProcess, rule
	case strings.Contains(rule, "."):
		return ruleDomain, rule
	}
	return ruleProcess, rule
}

func normalize(rule string) (string, bool) {
	domain := strings.ToLower(strings.TrimSuffix(rule, "."))
	if domain == "" || len(domain) > 253 || isAddr(domain) || strings.HasSuffix(domain, ".exe") {
		return "", false
	}
	for label := range strings.SplitSeq(domain, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", false
		}
		for _, char := range label {
			if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '-' || char == '_' {
				continue
			}
			return "", false
		}
	}
	return domain, true
}

func prefix(raw string) (netip.Prefix, bool) {
	if prefix, err := netip.ParsePrefix(raw); err == nil {
		return prefix.Masked(), true
	}
	address, err := netip.ParseAddr(raw)
	if err != nil || address.Zone() != "" {
		return netip.Prefix{}, false
	}
	return netip.PrefixFrom(address, address.BitLen()), true
}
