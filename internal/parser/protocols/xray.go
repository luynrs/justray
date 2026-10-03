package protocols

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/luynrs/justray/internal/domain"
)

type xrayDoc struct {
	Remarks   string            `json:"remarks"`
	Outbounds []json.RawMessage `json:"outbounds"`
}

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
	XHTTPSettings     json.RawMessage   `json:"xhttpSettings"`
	SplitHTTPSettings json.RawMessage   `json:"splitHttpSettings"`
	TCPSettings       xrayTCPTransport  `json:"tcpSettings"`
	RawSettings       *xrayTCPTransport `json:"rawSettings"`
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

func ParseXray(raw []byte) ([]domain.Node, map[string]int, error) {
	if !hasOutboundField(raw, "protocol") {
		return nil, nil, ErrNotFormat
	}
	var docs []xrayDoc
	if err := json.Unmarshal(raw, &docs); err != nil {
		var doc xrayDoc
		if json.Unmarshal(raw, &doc) != nil {
			return nil, nil, errors.New("xray: invalid outbound fields")
		}
		docs = []xrayDoc{doc}
	}

	var nodes []domain.Node
	skipped := make(map[string]int)
	for _, doc := range docs {
		proxies := make([]xrayOutbound, 0, len(doc.Outbounds))
		for _, rawOutbound := range doc.Outbounds {
			var ob xrayOutbound
			if json.Unmarshal(rawOutbound, &ob) != nil || ob.Protocol == "" {
				skipped["xray: invalid outbound fields"]++
				continue
			}
			p := strings.ToLower(ob.Protocol)
			switch p {
			case "vless", "vmess", "trojan", "shadowsocks", "ss":
				proxies = append(proxies, ob)
			case "freedom", "blackhole", "dns", "loopback":
			default:
				skipped["unsupported"]++
			}
		}
		for _, ob := range proxies {
			name := cmp.Or(doc.Remarks, ob.Tag)
			if len(proxies) > 1 && doc.Remarks != "" && ob.Tag != "" && !strings.EqualFold(ob.Tag, "proxy") {
				name += " (" + ob.Tag + ")"
			}
			proto := strings.ToLower(ob.Protocol)
			switch proto {
			case "vless", "vmess":
				if len(ob.Settings.Servers) > 0 || len(ob.Settings.Vnext) == 0 {
					skipped["incompatible outbound settings"]++
					continue
				}
			case "shadowsocks", "ss":
				if len(ob.Settings.Vnext) > 0 || len(ob.Settings.Servers) == 0 {
					skipped["incompatible outbound settings"]++
					continue
				}
			case "trojan":
				if len(ob.Settings.Servers) == 0 && len(ob.Settings.Vnext) == 0 {
					skipped["incompatible outbound settings"]++
					continue
				}
			}
			for _, server := range ob.Settings.Servers {
				node := domain.Node{Name: name, Server: server.Address, Port: server.Port}
				switch proto {
				case "trojan":
					node.Protocol, node.Auth = domain.Trojan, domain.Auth{Password: server.Password}
				case "shadowsocks", "ss":
					node.Protocol, node.Auth = domain.SS, domain.Auth{Password: server.Password, Method: server.Method}
				default:
					continue
				}
				node, err := xrayNode(node, ob.StreamSettings)
				if err != nil {
					skipped[err.Error()]++
					continue
				}
				nodes = append(nodes, node)
			}
			for _, next := range ob.Settings.Vnext {
				if len(next.Users) == 0 {
					skipped["incompatible outbound settings"]++
					continue
				}
				for _, user := range next.Users {
					node := domain.Node{Name: name, Server: next.Address, Port: next.Port}
					switch proto {
					case "vmess":
						node.Protocol, node.Auth = domain.VMess, domain.Auth{UUID: user.ID, AlterID: user.AlterID, Method: cmp.Or(user.Security, "auto")}
					case "vless":
						node.Protocol, node.Auth = domain.VLESS, domain.Auth{UUID: user.ID, Flow: user.Flow}
					case "trojan":
						node.Protocol, node.Auth = domain.Trojan, domain.Auth{Password: cmp.Or(user.Password, user.ID)}
					default:
						continue
					}
					node, err := xrayNode(node, ob.StreamSettings)
					if err != nil {
						skipped[err.Error()]++
						continue
					}
					nodes = append(nodes, node)
				}
			}
		}
	}
	if len(nodes) == 0 && len(skipped) == 0 {
		return nil, nil, errors.New("no supported outbounds in xray config")
	}
	return nodes, skipped, nil
}

func xrayNode(node domain.Node, stream xrayStreamSettings) (domain.Node, error) {
	var err error
	node.Transport, err = xrayTransport(stream)
	if err != nil {
		return domain.Node{}, err
	}
	switch strings.ToLower(stream.Security) {
	case "reality":
		node.TLS = &domain.TLS{SNI: stream.RealitySettings.ServerName, Fingerprint: stream.RealitySettings.Fingerprint}
		if node.Protocol != domain.VMess {
			node.Reality = &domain.Reality{PublicKey: stream.RealitySettings.PublicKey, ShortID: stream.RealitySettings.ShortID}
		}
	case "tls":
		fingerprint, insecure := cleanFingerprint(stream.TLSSettings.Fingerprint, stream.TLSSettings.AllowInsecure)
		node.TLS = &domain.TLS{SNI: stream.TLSSettings.ServerName, Insecure: insecure, ALPN: stream.TLSSettings.ALPN, Fingerprint: fingerprint}
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
