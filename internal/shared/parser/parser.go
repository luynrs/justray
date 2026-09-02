package parser

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/luynrs/justray/internal/shared/domain"
	"github.com/luynrs/justray/internal/shared/parser/protocols"
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
	"anytls":     protocols.ParseAnyTLS,
	"socks5":     protocols.ParseSOCKS,
	"socks":      protocols.ParseSOCKS,
	"wireguard":  protocols.ParseWireGuard,
	"wg":         protocols.ParseWireGuard,
	"shadowtls":  protocols.ParseShadowTLS,
	"shadow-tls": protocols.ParseShadowTLS,
}

func parserFor(uri string) func(string) (domain.Node, error) {
	scheme, _, ok := strings.Cut(strings.TrimSpace(uri), "://")
	if !ok {
		return nil
	}
	return parsers[scheme]
}

func IsLink(s string) bool { return parserFor(s) != nil }

func ParseURI(uri string) (domain.Node, error) {
	parse := parserFor(uri)
	if parse == nil {
		return domain.Node{}, fmt.Errorf("unknown scheme in %.80q", uri)
	}
	n, err := parse(strings.TrimSpace(uri))
	if err != nil {
		return domain.Node{}, err
	}
	n.ID = protocols.NodeID(n)
	return n, nil
}

func ParseSubscription(raw []byte) ([]domain.Node, error) {
	body := bytes.TrimSpace(raw)
	if decoded, err := protocols.Unbase64(string(body)); err == nil {
		if nodes, err := parseSub(decoded); err == nil {
			return nodes, nil
		}
	}
	return parseSub(body)
}

func parseSub(body []byte) ([]domain.Node, error) {
	if nodes, err := protocols.ParseXray(body); err == nil {
		return nodes, nil
	}
	if nodes, err := protocols.ParseClash(body); err == nil {
		return nodes, nil
	}
	if nodes := parseURILines(body); len(nodes) > 0 {
		return nodes, nil
	}
	return nil, errors.New("unrecognized subscription format")
}

func parseURILines(raw []byte) []domain.Node {
	var nodes []domain.Node
	for line := range strings.Lines(string(raw)) {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' || strings.HasPrefix(line, "//") {
			continue
		}
		n, err := ParseURI(line)
		if err != nil {
			continue
		}
		nodes = append(nodes, n)
	}
	return nodes
}
