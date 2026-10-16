package parser

import (
	"errors"
	"net/url"
	"strings"

	"github.com/luynrs/justray/internal/domain"
)

var uriProtocols = map[string]domain.Proto{
	"vmess": domain.VMess, "vless": domain.VLESS, "trojan": domain.Trojan,
	"ss": domain.SS, "hysteria": domain.HY1, "hysteria2": domain.HY2, "hy2": domain.HY2,
	"tuic": domain.TUIC, "tuic5": domain.TUIC, "tuicv5": domain.TUIC,
	"anytls": domain.AnyTLS, "socks": domain.SOCKS, "socks5": domain.SOCKS,
	"wireguard": domain.WG, "wg": domain.WG, "vpn": domain.WG,
	"shadowtls": domain.Shadow, "shadow-tls": domain.Shadow, "stls": domain.Shadow,
	"http": domain.HTTP, "https": domain.HTTP,
}

func IsLink(uri string) bool {
	scheme, _, found := strings.Cut(strings.TrimSpace(uri), "://")
	protocol, supported := uriProtocols[strings.ToLower(scheme)]
	if !found || !supported {
		return false
	}
	if protocol == domain.HTTP {
		parsedURL, err := url.Parse(strings.TrimSpace(uri))
		return err == nil && parsedURL.Host != "" &&
			(parsedURL.Path == "" || parsedURL.Path == "/") &&
			(parsedURL.User != nil || parsedURL.Port() != "" && parsedURL.Fragment != "")
	}
	return true
}

func ParseURI(uri string) (domain.Node, error) {
	if !IsLink(uri) {
		return domain.Node{}, errors.New("unknown URI scheme")
	}
	node, err := parseURI(uri)
	if err == nil {
		err = validateNode(node)
	}
	if err != nil {
		return domain.Node{}, err
	}
	return node, nil
}

func parseURI(uri string) (domain.Node, error) {
	uri = strings.TrimSpace(uri)
	scheme, _, _ := strings.Cut(uri, "://")
	protocol := uriProtocols[strings.ToLower(scheme)]
	var node domain.Node
	var err error
	switch protocol {
	case domain.SS:
		node, err = parseShadowsocks(uri)
	case domain.VMess:
		node, err = parseVMess(uri)
	case domain.WG:
		if strings.EqualFold(scheme, "vpn") {
			node, err = parseAmnezia(uri)
		} else {
			node, err = parseWireGuard(uri)
		}
	default:
		node, err = parseURL(uri, protocol)
	}
	var urlError *url.Error
	if errors.As(err, &urlError) {
		err = errors.New("invalid URI")
	}
	return node, err
}
