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
	Remarks   string         `json:"remarks"`
	Outbounds []xrayOutbound `json:"outbounds"`
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
	AlterID  int    `json:"alterId"`
	Security string `json:"security"`
}

type xrayStreamSettings struct {
	Network           string              `json:"network"`
	Security          string              `json:"security"`
	RealitySettings   xrayRealitySettings `json:"realitySettings"`
	TLSSettings       xrayTLSSettings     `json:"tlsSettings"`
	WSSettings        xrayHTTPTransport   `json:"wsSettings"`
	GRPCSettings      xrayGRPCTransport   `json:"grpcSettings"`
	HTTPSettings      xrayHTTPTransport   `json:"httpSettings"`
	XHTTPSettings     xrayXHTTPTransport  `json:"xhttpSettings"`
	SplitHTTPSettings xrayXHTTPTransport  `json:"splitHttpSettings"`
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

type xrayXHTTPTransport struct {
	Path    string            `json:"path"`
	Host    string            `json:"host"`
	Mode    string            `json:"mode"`
	Headers map[string]string `json:"headers"`
}

type xrayGRPCTransport struct {
	ServiceName string `json:"serviceName"`
}

func ParseXray(raw []byte) ([]domain.Node, error) {
	var docs []xrayDoc
	if err := json.Unmarshal(raw, &docs); err != nil {
		var doc xrayDoc
		if json.Unmarshal(raw, &doc) != nil {
			return nil, fmt.Errorf("invalid xray json: %w", err)
		}
		docs = []xrayDoc{doc}
	}

	var nodes []domain.Node
	for _, doc := range docs {
		proxies := make([]xrayOutbound, 0, len(doc.Outbounds))
		for _, ob := range doc.Outbounds {
			p := strings.ToLower(ob.Protocol)
			if p == "vless" || p == "vmess" || p == "trojan" || p == "shadowsocks" || p == "ss" {
				proxies = append(proxies, ob)
			}
		}
		for _, ob := range proxies {
			name := cmp.Or(doc.Remarks, ob.Tag)
			if len(proxies) > 1 && doc.Remarks != "" && ob.Tag != "" && !strings.EqualFold(ob.Tag, "proxy") {
				name += " (" + ob.Tag + ")"
			}
			proto := strings.ToLower(ob.Protocol)
			for _, s := range ob.Settings.Servers {
				var node domain.Node
				var err error
				switch proto {
				case "trojan":
					node, err = parseXrayTrojan(ob.StreamSettings, s, name)
				case "shadowsocks", "ss":
					node, err = parseXraySS(s, name)
				}
				if err == nil {
					node.ID = NodeID(node)
					nodes = append(nodes, node)
				}
			}
			for _, next := range ob.Settings.Vnext {
				for _, user := range next.Users {
					var node domain.Node
					var err error
					switch proto {
					case "vmess":
						node, err = parseXrayVMess(ob.StreamSettings, next, user, name)
					case "vless":
						node, err = parseXrayVLess(ob.StreamSettings, next, user, name)
					case "trojan":
						node, err = parseXrayTrojan(ob.StreamSettings, xrayServer{Address: next.Address, Port: next.Port, Password: cmp.Or(user.Password, user.ID)}, name)
					}
					if err == nil {
						node.ID = NodeID(node)
						nodes = append(nodes, node)
					}
				}
			}
		}
	}
	if len(nodes) == 0 {
		return nil, errors.New("no supported outbounds in xray config")
	}
	return nodes, nil
}

func parseXrayVLess(stream xrayStreamSettings, next xrayVnext, user xrayUser, name string) (domain.Node, error) {
	if next.Address == "" || !domain.ValidPort(next.Port) || user.ID == "" {
		return domain.Node{}, errors.New("vless: missing server, port, or uuid")
	}
	transport, err := xrayTransport(stream)
	if err != nil {
		return domain.Node{}, err
	}
	return domain.Node{
		Name: name, Protocol: domain.VLess, Server: next.Address, Port: next.Port,
		Auth: domain.Auth{UUID: user.ID, Flow: user.Flow}, Transport: transport,
		TLS: xrayTLS(stream), Reality: xrayReality(stream),
	}, nil
}

func parseXrayVMess(stream xrayStreamSettings, next xrayVnext, user xrayUser, name string) (domain.Node, error) {
	if next.Address == "" || !domain.ValidPort(next.Port) || user.ID == "" {
		return domain.Node{}, errors.New("vmess: missing server, port, or uuid")
	}
	transport, err := xrayTransport(stream)
	if err != nil {
		return domain.Node{}, err
	}
	return domain.Node{
		Name: name, Protocol: domain.VMess, Server: next.Address, Port: next.Port,
		Auth: domain.Auth{UUID: user.ID, AlterID: user.AlterID, Method: cmp.Or(user.Security, "auto")},
		Transport: transport, TLS: xrayTLS(stream),
	}, nil
}

func parseXrayTrojan(stream xrayStreamSettings, s xrayServer, name string) (domain.Node, error) {
	if s.Address == "" || !domain.ValidPort(s.Port) || s.Password == "" {
		return domain.Node{}, errors.New("trojan: missing server, port, or password")
	}
	transport, err := xrayTransport(stream)
	if err != nil {
		return domain.Node{}, err
	}
	tls := xrayTLS(stream)
	if tls == nil && stream.Security == "" {
		tls = &domain.TLS{SNI: s.Address}
	}
	return domain.Node{
		Name: name, Protocol: domain.Trojan, Server: s.Address, Port: s.Port,
		Auth: domain.Auth{Password: s.Password}, Transport: transport,
		TLS: tls, Reality: xrayReality(stream),
	}, nil
}

func parseXraySS(s xrayServer, name string) (domain.Node, error) {
	if s.Address == "" || !domain.ValidPort(s.Port) || s.Password == "" || s.Method == "" {
		return domain.Node{}, errors.New("shadowsocks: missing server, port, password, or method")
	}
	return domain.Node{
		Name: name, Protocol: domain.SS, Server: s.Address, Port: s.Port,
		Auth: domain.Auth{Password: s.Password, Method: s.Method},
	}, nil
}

func xrayTransport(s xrayStreamSettings) (domain.Transport, error) {
	switch network := strings.ToLower(s.Network); network {
	case "", "tcp", "raw":
		return domain.Transport{Network: "tcp"}, nil
	case "ws":
		return domain.Transport{Network: network, Path: s.WSSettings.Path, Host: cmp.Or(s.WSSettings.Host, s.WSSettings.Headers["Host"])}, nil
	case "grpc":
		return domain.Transport{Network: network, ServiceName: s.GRPCSettings.ServiceName}, nil
	case "http", "h2":
		return domain.Transport{Network: "http", Path: s.HTTPSettings.Path, Host: cmp.Or(s.HTTPSettings.Host, s.HTTPSettings.Headers["Host"])}, nil
	case "xhttp", "splithttp":
		return domain.Transport{
			Network: "xhttp",
			Path:    cmp.Or(s.XHTTPSettings.Path, s.SplitHTTPSettings.Path),
			Host:    cmp.Or(s.XHTTPSettings.Host, s.SplitHTTPSettings.Host, s.XHTTPSettings.Headers["Host"], s.XHTTPSettings.Headers["host"], s.SplitHTTPSettings.Headers["Host"], s.SplitHTTPSettings.Headers["host"]),
			Mode:    cmp.Or(s.XHTTPSettings.Mode, s.SplitHTTPSettings.Mode),
		}, nil
	default:
		return domain.Transport{}, fmt.Errorf("unsupported xray transport: %s", network)
	}
}

func xrayTLS(s xrayStreamSettings) *domain.TLS {
	switch strings.ToLower(s.Security) {
	case "reality":
		return &domain.TLS{SNI: s.RealitySettings.ServerName, Fingerprint: s.RealitySettings.Fingerprint}
	case "tls":
		fp, insecure := cleanFingerprint(s.TLSSettings.Fingerprint, s.TLSSettings.AllowInsecure)
		return &domain.TLS{SNI: s.TLSSettings.ServerName, Insecure: insecure, ALPN: s.TLSSettings.ALPN, Fingerprint: fp}
	}
	return nil
}

func xrayReality(s xrayStreamSettings) *domain.Reality {
	if !strings.EqualFold(s.Security, "reality") || s.RealitySettings.PublicKey == "" {
		return nil
	}
	return &domain.Reality{PublicKey: s.RealitySettings.PublicKey, ShortID: s.RealitySettings.ShortID}
}
