package parser

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strings"

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

func ParseSubscription(raw []byte) ([]domain.Node, error) {
	if decoded, err := protocols.Unbase64(string(raw)); err == nil {
		nodes, err := parseSub(decoded)
		if err != protocols.ErrNotFormat {
			return nodes, err
		}
	}
	return parseSub(raw)
}

func parseSub(body []byte) ([]domain.Node, error) {
	body = bytes.TrimPrefix(bytes.TrimSpace(body), []byte("\xef\xbb\xbf"))
	for _, parse := range []func([]byte) ([]domain.Node, error){protocols.ParseSingBox, protocols.ParseXray, protocols.ParseClash} {
		nodes, err := parse(body)
		if err == protocols.ErrNotFormat {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, node := range nodes {
			if err := validateNode(node); err != nil {
				return nil, err
			}
		}
		return nodes, nil
	}
	if len(body) > 0 && (body[0] == '{' || body[0] == '[') && !json.Valid(body) {
		return nil, errors.New("invalid JSON subscription")
	}
	if nodes, err := parseURILines(body); len(nodes) > 0 || err != nil {
		return nodes, err
	}
	return nil, protocols.ErrNotFormat
}

func validateNode(node domain.Node) error {
	if node.Server == "" || !domain.ValidPort(node.Port) {
		return errors.New("missing server or valid port")
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

func parseURILines(raw []byte) ([]domain.Node, error) {
	var nodes []domain.Node
	for line := range strings.Lines(string(raw)) {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' || strings.HasPrefix(line, "//") {
			continue
		}
		n, err := ParseURI(line)
		if err != nil {
			scheme, _, hasScheme := strings.Cut(line, "://")
			if IsLink(line) || hasScheme && !strings.EqualFold(scheme, "http") && !strings.EqualFold(scheme, "https") {
				return nil, err
			}
			continue
		}
		nodes = append(nodes, n)
	}
	return nodes, nil
}
