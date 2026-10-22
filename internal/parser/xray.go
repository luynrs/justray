package parser

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"

	"github.com/luynrs/justray/internal/domain"
)

type xrayOutbound struct {
	Tag            string             `json:"tag"`
	Protocol       string             `json:"protocol"`
	Settings       xraySettings       `json:"settings"`
	StreamSettings xrayStreamSettings `json:"streamSettings"`
}

type xraySettings struct {
	Vnext   []xrayVnext  `json:"vnext"`
	Servers []xrayServer `json:"servers"`
}

type xrayVnext struct {
	Address string     `json:"address"`
	Port    int        `json:"port"`
	Users   []xrayUser `json:"users"`
}

type xrayServer struct {
	Address  string `json:"address"`
	Port     int    `json:"port"`
	Password string `json:"password"`
	Method   string `json:"method"`
}

type xrayUser struct {
	ID       string `json:"id"`
	Password string `json:"password"`
	Flow     string `json:"flow"`
	Security string `json:"security"`
	AlterID  int    `json:"alterId"`
}

type xrayStreamSettings struct {
	Network         string              `json:"network"`
	Method          string              `json:"method"`
	Security        string              `json:"security"`
	RealitySettings xrayRealitySettings `json:"realitySettings"`
	TLSSettings     xrayTLSSettings     `json:"tlsSettings"`
	WSSettings      xrayHTTPTransport   `json:"wsSettings"`
	GRPCSettings    xrayGRPCTransport   `json:"grpcSettings"`
	HTTPSettings    struct {
		Path    string                   `json:"path"`
		Host    stringOrSlice            `json:"host"`
		Headers map[string]stringOrSlice `json:"headers"`
	} `json:"httpSettings"`
	XHTTPSettings       json.RawMessage   `json:"xhttpSettings"`
	SplitHTTPSettings   json.RawMessage   `json:"splitHttpSettings"`
	TCPSettings         xrayTCPTransport  `json:"tcpSettings"`
	RawSettings         *xrayTCPTransport `json:"rawSettings"`
	HTTPUpgradeSettings xrayHTTPTransport `json:"httpupgradeSettings"`
}

type xrayRealitySettings struct {
	ServerName  string `json:"serverName"`
	PublicKey   string `json:"publicKey"`
	ShortID     string `json:"shortId"`
	Fingerprint string `json:"fingerprint"`
}

type xrayTLSSettings struct {
	ServerName    string   `json:"serverName"`
	AllowInsecure bool     `json:"allowInsecure"`
	ALPN          []string `json:"alpn"`
	Fingerprint   string   `json:"fingerprint"`
}

type xrayHTTPTransport struct {
	Path    string            `json:"path"`
	Host    string            `json:"host"`
	Headers map[string]string `json:"headers"`
}

type xrayGRPCTransport struct {
	ServiceName string `json:"serviceName"`
}

type xrayTCPTransport struct {
	Header struct {
		Type string `json:"type"`
	} `json:"header"`
}

func (result *parseResult) readXray(outbound xrayOutbound, remarks string, count int) {
	name := cmp.Or(remarks, outbound.Tag)
	if count > 1 && remarks != "" && outbound.Tag != "" && !strings.EqualFold(outbound.Tag, "proxy") {
		name += " (" + outbound.Tag + ")"
	}
	protocol := strings.ToLower(outbound.Protocol)
	switch protocol {
	case "vless", "vmess":
		if len(outbound.Settings.Servers) > 0 || len(outbound.Settings.Vnext) == 0 {
			result.add(domain.Node{}, errors.New("incompatible outbound settings"))
			return
		}
	case "shadowsocks", "ss":
		if len(outbound.Settings.Vnext) > 0 || len(outbound.Settings.Servers) == 0 {
			result.add(domain.Node{}, errors.New("incompatible outbound settings"))
			return
		}
	case "trojan":
		if len(outbound.Settings.Servers) == 0 && len(outbound.Settings.Vnext) == 0 {
			result.add(domain.Node{}, errors.New("incompatible outbound settings"))
			return
		}
	}
	for _, server := range outbound.Settings.Servers {
		node := domain.Node{Name: name, Server: server.Address, Port: server.Port}
		switch protocol {
		case "trojan":
			node.Protocol, node.Auth = domain.Trojan, domain.Auth{Password: server.Password}
		case "shadowsocks", "ss":
			node.Protocol, node.Auth = domain.SS, domain.Auth{Password: server.Password, Method: server.Method}
		}
		result.add(xrayNode(node, outbound.StreamSettings))
	}
	for _, next := range outbound.Settings.Vnext {
		if len(next.Users) == 0 {
			result.add(domain.Node{}, errors.New("incompatible outbound settings"))
			continue
		}
		for _, user := range next.Users {
			node := domain.Node{Name: name, Server: next.Address, Port: next.Port}
			switch protocol {
			case "vmess":
				node.Protocol, node.Auth = domain.VMess, domain.Auth{UUID: user.ID, AlterID: user.AlterID, Method: cmp.Or(user.Security, "auto")}
			case "vless":
				node.Protocol, node.Auth = domain.VLESS, domain.Auth{UUID: user.ID, Flow: user.Flow}
			case "trojan":
				node.Protocol, node.Auth = domain.Trojan, domain.Auth{Password: cmp.Or(user.Password, user.ID)}
			}
			result.add(xrayNode(node, outbound.StreamSettings))
		}
	}
}

func xrayNode(node domain.Node, stream xrayStreamSettings) (domain.Node, error) {
	var err error
	node.Transport, err = xrayTransport(stream)
	if err != nil {
		return domain.Node{}, err
	}
	switch security := strings.ToLower(stream.Security); security {
	case "reality":
		node.TLS = &domain.TLS{SNI: stream.RealitySettings.ServerName, Fingerprint: stream.RealitySettings.Fingerprint}
		if node.Protocol != domain.VMess {
			node.Reality = &domain.Reality{PublicKey: stream.RealitySettings.PublicKey, ShortID: stream.RealitySettings.ShortID}
		}
	case "tls":
		fingerprint, insecure := cleanFingerprint(stream.TLSSettings.Fingerprint, stream.TLSSettings.AllowInsecure)
		node.TLS = &domain.TLS{SNI: stream.TLSSettings.ServerName, Insecure: insecure, ALPN: stream.TLSSettings.ALPN, Fingerprint: fingerprint}
	case "", "none":
	default:
		return domain.Node{}, fmt.Errorf("unsupported xray security: %s", security)
	}
	if node.Protocol == domain.Trojan && node.TLS == nil && stream.Security == "" {
		node.TLS = &domain.TLS{SNI: node.Server}
	}
	return node, nil
}

func xrayTransport(s xrayStreamSettings) (domain.Transport, error) {
	switch network := strings.ToLower(cmp.Or(s.Method, s.Network)); network {
	case "", "tcp", "raw":
		if s.RawSettings != nil {
			s.TCPSettings = *s.RawSettings
		}
		return domain.Transport{Network: "tcp", Mode: strings.ToLower(s.TCPSettings.Header.Type)}, nil
	case "ws":
		return domain.Transport{Network: network, Path: s.WSSettings.Path, Host: cmp.Or(s.WSSettings.Host, s.WSSettings.Headers["Host"])}, nil
	case "httpupgrade":
		transport := domain.Transport{Network: network, Path: xrayHTTPUpgradePath(s.HTTPUpgradeSettings.Path), Host: s.HTTPUpgradeSettings.Host}
		headers := maps.Clone(s.HTTPUpgradeSettings.Headers)
		for name, value := range headers {
			if strings.EqualFold(name, "Host") {
				transport.Host = cmp.Or(transport.Host, value)
				delete(headers, name)
			}
		}
		if len(headers) > 0 {
			data, _ := json.Marshal(map[string]any{"headers": headers})
			transport.Extra = string(data)
		}
		return transport, nil
	case "grpc":
		return domain.Transport{Network: network, ServiceName: s.GRPCSettings.ServiceName}, nil
	case "http", "h2":
		transport := domain.Transport{Network: "http", Path: s.HTTPSettings.Path}
		if len(s.HTTPSettings.Host) > 0 {
			transport.Host = s.HTTPSettings.Host[0]
		} else if hosts := s.HTTPSettings.Headers["Host"]; len(hosts) > 0 {
			transport.Host = hosts[0]
		}
		return transport, nil
	case "xhttp", "splithttp":
		return domain.Transport{Network: "xhttp", Extra: cmp.Or(string(s.XHTTPSettings), string(s.SplitHTTPSettings))}, nil
	default:
		return domain.Transport{}, fmt.Errorf("unsupported xray transport: %s", network)
	}
}
