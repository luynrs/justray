package parser

import (
	"cmp"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/luynrs/justray/internal/domain"
)

func parseURL(uri string, protocol domain.Proto) (domain.Node, error) {
	parsedURL, err := url.Parse(uri)
	if err != nil {
		return domain.Node{}, fmt.Errorf("%s: %w", protocol, err)
	}
	var host string
	var port int
	if protocol == domain.HTTP {
		host, port = parsedURL.Hostname(), 80
		if strings.EqualFold(parsedURL.Scheme, "https") {
			port = 443
		}
		if value := parsedURL.Port(); value != "" {
			port, err = strconv.Atoi(value)
		}
	} else {
		host, port, err = parseHostPort(parsedURL.Host)
	}
	if err != nil {
		return domain.Node{}, fmt.Errorf("%s: invalid host or port", protocol)
	}
	query := parsedURL.Query()
	node := domain.Node{
		Name:     cmp.Or(parsedURL.Fragment, host),
		Protocol: protocol,
		Server:   host,
		Port:     port,
	}
	switch protocol {
	case domain.VLESS:
		// vless://uuid@host:port?...#remark
		node.Auth.UUID = parsedURL.User.Username()
		node.Auth.Flow = query.Get("flow")
		node.Transport = parseTransport(query)
		node.PacketEncoding = cmp.Or(query.Get("packetEncoding"), query.Get("packet_encoding"), query.Get("packet-encoding"))
		publicKey := cmp.Or(query.Get("pbk"), query.Get("publicKey"), query.Get("public_key"), query.Get("pubkey"))
		security := strings.ToLower(query.Get("security"))
		if security == "" && publicKey != "" {
			security = "reality"
		} else if security == "" && (truthy(query.Get("tls")) || query.Get("sni") != "") {
			security = "tls"
		}
		switch security {
		case "reality":
			node.Reality = &domain.Reality{PublicKey: publicKey, ShortID: cmp.Or(query.Get("sid"), query.Get("shortId"), query.Get("short_id"))}
			fallthrough
		case "tls", "xtls":
			node.TLS = parseTLS(query, host)
		case "", "none":
		default:
			return domain.Node{}, fmt.Errorf("%s: unsupported security", protocol)
		}
	case domain.Trojan, domain.AnyTLS:
		node.Auth.Password = cmp.Or(userPassword(parsedURL), query.Get("password"), query.Get("auth"))
		if protocol == domain.Trojan {
			node.Transport = parseTransport(query)
		}
		if protocol == domain.AnyTLS || !strings.EqualFold(query.Get("security"), "none") {
			node.TLS = parseTLS(query, host)
		}
	case domain.HY1:
		node.Auth.Password = cmp.Or(userPassword(parsedURL), query.Get("auth"), query.Get("auth_str"), query.Get("password"))
		node.TLS = parseTLS(query, host)
		node.ObfsPassword = cmp.Or(query.Get("obfsParam"), query.Get("obfs_param"), query.Get("obfs-param"))
		if node.ObfsPassword == "" && query.Get("obfs") != "xplus" {
			node.ObfsPassword = query.Get("obfs")
		}
		for _, bandwidth := range []struct {
			field string
			value *int
		}{{"upmbps", &node.UpMbps}, {"downmbps", &node.DownMbps}} {
			if value := strings.TrimSpace(query.Get(bandwidth.field)); value != "" {
				*bandwidth.value, err = strconv.Atoi(value)
				if err != nil {
					return domain.Node{}, fmt.Errorf("%s: invalid %s", protocol, bandwidth.field)
				}
			}
			*bandwidth.value = cmp.Or(*bandwidth.value, 100)
		}
	case domain.HY2:
		auth, _ := url.PathUnescape(strings.TrimPrefix(parsedURL.User.String(), ":"))
		node.Auth.Password = cmp.Or(auth, query.Get("auth"), query.Get("password"))
		node.TLS = parseTLS(query, host)
		node.ObfsPassword = cmp.Or(query.Get("obfs-password"), query.Get("obfs_password"), query.Get("obfs-param"), query.Get("obfsparam"))
		node.Obfs = query.Get("obfs")
		if node.Obfs == "" && node.ObfsPassword != "" {
			node.Obfs = "salamander"
		}
	case domain.HTTP, domain.SOCKS:
		node.Auth.Username = parsedURL.User.Username()
		node.Auth.Password, _ = parsedURL.User.Password()
		if protocol == domain.SOCKS && node.Auth.Password == "" && node.Auth.Username != "" {
			node.Auth.Username, node.Auth.Password = parseCredentials(node.Auth.Username)
		}
		if protocol == domain.HTTP && (strings.EqualFold(parsedURL.Scheme, "https") || truthy(query.Get("tls")) || query.Get("security") == "tls") {
			node.TLS = parseTLS(query, host)
		}
	case domain.TUIC:
		// TUIC v5
		node.Auth.UUID = parsedURL.User.Username()
		password, _ := parsedURL.User.Password()
		node.Auth.Password = cmp.Or(password, query.Get("password"))
		node.TLS = parseTLS(query, host)
		if len(node.TLS.ALPN) == 0 {
			node.TLS.ALPN = []string{"h3"}
		}
		node.Congestion = cmp.Or(query.Get("congestion_control"), query.Get("congestion-control"), query.Get("congestion"), "bbr")
		node.UDPRelayMode = cmp.Or(query.Get("udp_relay_mode"), query.Get("udp-relay-mode"), "native")
	case domain.Shadow:
		// shadowtls://password@host:port?version=3&sni=example.com#remark
		version := 3
		if value := strings.TrimSpace(query.Get("version")); value != "" {
			version, err = strconv.Atoi(value)
			if err != nil {
				return domain.Node{}, errors.New("shadowtls: invalid version")
			}
		}
		node.TLS = parseTLS(query, cmp.Or(query.Get("host"), host))
		node.ShadowTLS = &domain.ShadowTLS{
			Version:  cmp.Or(version, 3),
			Password: cmp.Or(userPassword(parsedURL), query.Get("password"), query.Get("auth")),
			SNI:      node.TLS.SNI,
		}
	}
	return node, nil
}
