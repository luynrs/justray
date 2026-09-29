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
			for _, s := range ob.Settings.Servers {
				var node domain.Node
				var err error
				switch proto {
				case "trojan":
					node, err = parseXrayTrojan(ob.StreamSettings, s, name)
				case "shadowsocks", "ss":
					node = domain.Node{
						Name: name, Protocol: domain.SS, Server: s.Address, Port: s.Port,
						Auth: domain.Auth{Password: s.Password, Method: s.Method},
					}
				default:
					continue
				}
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
					var node domain.Node
					var err error
					switch proto {
					case "vmess":
						node, err = parseXrayVMess(ob.StreamSettings, next, user, name)
					case "vless":
						node, err = parseXrayVLess(ob.StreamSettings, next, user, name)
					case "trojan":
						node, err = parseXrayTrojan(ob.StreamSettings, xrayServer{Address: next.Address, Port: next.Port, Password: cmp.Or(user.Password, user.ID)}, name)
					default:
						continue
					}
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

func parseXrayVLess(stream xrayStreamSettings, next xrayVnext, user xrayUser, name string) (domain.Node, error) {
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
	transport, err := xrayTransport(stream)
	if err != nil {
		return domain.Node{}, err
	}
	return domain.Node{
		Name: name, Protocol: domain.VMess, Server: next.Address, Port: next.Port,
		Auth:      domain.Auth{UUID: user.ID, Method: cmp.Or(user.Security, "auto")},
		Transport: transport, TLS: xrayTLS(stream),
	}, nil
}

func parseXrayTrojan(stream xrayStreamSettings, s xrayServer, name string) (domain.Node, error) {
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
