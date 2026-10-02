package parser

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/engine/outbound"
	"github.com/luynrs/justray/internal/parser/protocols"
)

var parsers = map[string]func(string) (domain.Node, error){
	"vmess":      protocols.ParseVMess,
	"vless":      protocols.ParseVLess,
	"trojan":     protocols.ParseTrojan,
	"ss":         protocols.ParseShadowsocks,
	"hysteria":   protocols.ParseHysteria,
	"hysteria2":  protocols.ParseHysteria2,
	"hy2":        protocols.ParseHysteria2,
	"tuic":       protocols.ParseTUIC,
	"tuic5":      protocols.ParseTUIC,
	"tuicv5":     protocols.ParseTUIC,
	"anytls":     protocols.ParseAnyTLS,
	"socks5":     protocols.ParseSOCKS,
	"socks":      protocols.ParseSOCKS,
	"wireguard":  protocols.ParseWireGuard,
	"wg":         protocols.ParseWireGuard,
	"shadowtls":  protocols.ParseShadowTLS,
	"shadow-tls": protocols.ParseShadowTLS,
	"stls":       protocols.ParseShadowTLS,
	"http":       protocols.ParseHTTP,
	"https":      protocols.ParseHTTP,
}

func parserFor(uri string) func(string) (domain.Node, error) {
	scheme, _, ok := strings.Cut(strings.TrimSpace(uri), "://")
	if !ok {
		return nil
	}
	scheme = strings.ToLower(scheme)
	if (scheme == "http" || scheme == "https") && !isHTTPLink(uri) {
		return nil
	}
	return parsers[scheme]
}

func IsLink(s string) bool { return parserFor(s) != nil }

func isHTTPLink(s string) bool {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || u.Host == "" {
		return false
	}
	if u.Path != "" && u.Path != "/" {
		return false
	}
	return u.User != nil || (u.Port() != "" && u.Fragment != "")
}

func ParseURI(uri string) (domain.Node, error) {
	parse := parserFor(uri)
	if parse == nil {
		return domain.Node{}, errors.New("unknown URI scheme")
	}
	node, err := parse(strings.TrimSpace(uri))
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			return domain.Node{}, fmt.Errorf("invalid URI: %w", urlErr.Err)
		}
		return domain.Node{}, err
	}
	if err := validateNode(node); err != nil {
		return domain.Node{}, err
	}
	return node, nil
}

func ParseSubscription(raw []byte) ([]domain.Node, string, error) {
	if decoded, err := protocols.Unbase64(string(raw)); err == nil {
		nodes, warning, err := parseSub(decoded)
		if err != protocols.ErrNotFormat {
			return nodes, warning, err
		}
	}
	return parseSub(raw)
}

func parseSub(body []byte) ([]domain.Node, string, error) {
	body = bytes.TrimPrefix(bytes.TrimSpace(body), []byte("\xef\xbb\xbf"))
	for _, parse := range []func([]byte) ([]domain.Node, map[string]int, error){protocols.ParseSingBox, protocols.ParseXray, protocols.ParseClash} {
		nodes, skipped, err := parse(body)
		if err == protocols.ErrNotFormat {
			continue
		}
		if err != nil {
			return nil, "", err
		}
		valid := nodes[:0]
		for _, node := range nodes {
			if err := validateNode(node); err != nil {
				skipped[err.Error()]++
				continue
			}
			valid = append(valid, node)
		}
		return subscriptionResult(valid, skipped)
	}
	if len(body) > 0 && (body[0] == '{' || body[0] == '[') && !json.Valid(body) {
		return nil, "", errors.New("invalid JSON subscription")
	}
	if nodes, warning, err := parseURILines(body); len(nodes) > 0 || err != nil {
		return nodes, warning, err
	}
	return nil, "", protocols.ErrNotFormat
}

func validateNode(node domain.Node) error {
	if !utf8.ValidString(node.Auth.Password) || !utf8.ValidString(node.Auth.Username) || !utf8.ValidString(node.ObfsPassword) || node.ShadowTLS != nil && !utf8.ValidString(node.ShadowTLS.Password) {
		return errors.New("non-UTF8 credentials are not supported")
	}
	if node.TLS != nil && node.TLS.Insecure {
		return errors.New("insecure TLS is not supported")
	}
	if (node.Protocol == domain.SOCKS || node.Protocol == domain.SS && node.ShadowTLS == nil) && (node.TLS != nil || node.Reality != nil) {
		return fmt.Errorf("%s: TLS is not supported", node.Protocol)
	}
	if node.Reality != nil && node.Reality.PublicKey == "" {
		return errors.New("reality: missing public key")
	}
	if node.Server == "" || !domain.ValidPort(node.Port) {
		return errors.New("missing host or valid port")
	}
	switch node.Protocol {
	case domain.VLess, domain.VMess, domain.TUIC:
		if node.Auth.UUID == "" {
			return fmt.Errorf("%s: missing uuid", node.Protocol)
		}
	case domain.Trojan, domain.HY2, domain.AnyTLS:
		if node.Auth.Password == "" {
			return fmt.Errorf("%s: missing password", node.Protocol)
		}
	case domain.SS:
		if node.Auth.Method == "" || node.Auth.Password == "" {
			return errors.New("shadowsocks: missing method or password")
		}
		if node.ShadowTLS != nil && node.ShadowTLS.Password == "" {
			return errors.New("shadowtls: missing password")
		}
	case domain.Shadow:
		if node.ShadowTLS == nil || node.ShadowTLS.Password == "" {
			return errors.New("shadowtls: missing settings or password")
		}
	case domain.WG:
		if node.WireGuard == nil || node.WireGuard.PrivateKey == "" || node.WireGuard.PeerPublicKey == "" || len(node.WireGuard.Address) == 0 {
			return errors.New("wireguard: missing keys or address")
		}
		if len(node.WireGuard.Reserved) != 0 && len(node.WireGuard.Reserved) != 3 {
			return errors.New("wireguard: reserved must be three bytes")
		}
		for _, address := range node.WireGuard.Address {
			if _, err := netip.ParsePrefix(address); err != nil {
				return fmt.Errorf("wireguard: invalid address: %w", err)
			}
		}
	case domain.HY1, domain.SOCKS, domain.HTTP:
	default:
		return fmt.Errorf("unsupported protocol %q", node.Protocol)
	}
	return outbound.ValidateTransport(node)
}

func parseURILines(raw []byte) ([]domain.Node, string, error) {
	var nodes []domain.Node
	skipped := make(map[string]int)
	for line := range strings.Lines(string(raw)) {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' || strings.HasPrefix(line, "//") {
			continue
		}
		n, err := ParseURI(line)
		if err != nil {
			scheme, _, hasScheme := strings.Cut(line, "://")
			if IsLink(line) {
				skipped[err.Error()]++
			} else if hasScheme && !strings.EqualFold(scheme, "http") && !strings.EqualFold(scheme, "https") {
				skipped["unsupported"]++
			}
			continue
		}
		nodes = append(nodes, n)
	}
	return subscriptionResult(nodes, skipped)
}

func subscriptionResult(nodes []domain.Node, skipped map[string]int) ([]domain.Node, string, error) {
	if len(skipped) == 0 {
		return nodes, "", nil
	}
	var reasons []string
	var total int
	for reason, count := range skipped {
		total += count
		reasons = append(reasons, fmt.Sprintf("%s: %d", reason, count))
	}
	slices.Sort(reasons)
	warning := fmt.Sprintf("skipped nodes: %d (%s)", total, strings.Join(reasons, "; "))
	if len(nodes) == 0 {
		return nil, "", fmt.Errorf("no supported nodes; %s", warning)
	}
	return nodes, warning, nil
}
