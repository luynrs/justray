package protocols

import (
	"cmp"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/luynrs/justray/internal/shared/domain"
)

type clashProxy struct {
	Name              string   `yaml:"name"`
	Type              string   `yaml:"type"`
	Server            string   `yaml:"server"`
	Port              int      `yaml:"port"`
	UUID              string   `yaml:"uuid"`
	Password          string   `yaml:"password"`
	Cipher            string   `yaml:"cipher"`
	AlterID           int      `yaml:"alterId"`
	Network           string   `yaml:"network"`
	TLS               bool     `yaml:"tls"`
	SkipCertVerify    bool     `yaml:"skip-cert-verify"`
	ServerName        string   `yaml:"servername"`
	SNI               string   `yaml:"sni"`
	Flow              string   `yaml:"flow"`
	ClientFingerprint string   `yaml:"client-fingerprint"`
	ALPN              []string `yaml:"alpn"`
	Obfs              string   `yaml:"obfs"`
	ObfsPassword      string   `yaml:"obfs-password"`
	Username          string   `yaml:"username"`
	AuthStr           string   `yaml:"auth-str"`
	AuthStrOld        string   `yaml:"auth_str"`
	Up                mbps     `yaml:"up"`
	Down              mbps     `yaml:"down"`
	Congestion        string   `yaml:"congestion-controller"`
	UDPRelayMode      string   `yaml:"udp-relay-mode"`
	PacketEncoding    string   `yaml:"packet-encoding"`

	PrivateKey   string   `yaml:"private-key"`
	PublicKey    string   `yaml:"public-key"`
	PreSharedKey string   `yaml:"pre-shared-key"`
	IP           string   `yaml:"ip"`
	IPv6         string   `yaml:"ipv6"`
	MTU          uint32   `yaml:"mtu"`
	Reserved     reserved `yaml:"reserved"`

	Plugin     string `yaml:"plugin"`
	PluginOpts *struct {
		Host     string `yaml:"host"`
		Password string `yaml:"password"`
		Version  int    `yaml:"version"`
	} `yaml:"plugin-opts"`

	WSOpts *struct {
		Path    string            `yaml:"path"`
		Headers map[string]string `yaml:"headers"`
	} `yaml:"ws-opts"`
	GRPCOpts *struct {
		ServiceName string `yaml:"grpc-service-name"`
	} `yaml:"grpc-opts"`
	XHTTPOpts *struct {
		Path    string            `yaml:"path"`
		Host    string            `yaml:"host"`
		Headers map[string]string `yaml:"headers"`
		Mode    string            `yaml:"mode"`
	} `yaml:"xhttp-opts"`
	RealityOpts *struct {
		PublicKey string `yaml:"public-key"`
		ShortID   string `yaml:"short-id"`
	} `yaml:"reality-opts"`
}

// Clash/Mihomo "proxies:" list
func ParseClash(raw []byte) ([]domain.Node, error) {
	var doc struct {
		Proxies []yaml.Node `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("clash: %w", err)
	}

	var nodes []domain.Node
	for _, raw := range doc.Proxies {
		var p clashProxy
		if err := raw.Decode(&p); err != nil {
			continue
		}
		if n, err := clashNode(p); err == nil {
			nodes = append(nodes, n)
		}
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("clash: no supported proxies")
	}
	return nodes, nil
}

func clashNode(p clashProxy) (domain.Node, error) {
	if p.Server == "" || !domain.ValidPort(p.Port) {
		return domain.Node{}, fmt.Errorf("clash: missing server/port")
	}
	n := domain.Node{
		Name:   cmp.Or(p.Name, p.Server),
		Server: p.Server,
		Port:   p.Port,
	}
	tls := &domain.TLS{
		SNI:         cmp.Or(p.SNI, p.ServerName, p.Server),
		ALPN:        p.ALPN,
		Fingerprint: p.ClientFingerprint,
		Insecure:    p.SkipCertVerify,
	}

	switch strings.ToLower(p.Type) {
	case "vless":
		if p.UUID == "" {
			return domain.Node{}, fmt.Errorf("clash: vless missing uuid")
		}
		n.Protocol = domain.VLess
		n.Auth = domain.Auth{UUID: p.UUID, Flow: p.Flow}
		n.Transport = clashTransport(p)
		n.PacketEncoding = p.PacketEncoding
		if p.TLS || p.RealityOpts != nil {
			n.TLS = tls
		}
		if p.RealityOpts != nil {
			n.Reality = &domain.Reality{PublicKey: p.RealityOpts.PublicKey, ShortID: p.RealityOpts.ShortID}
		}

	case "vmess":
		if p.UUID == "" {
			return domain.Node{}, fmt.Errorf("clash: vmess missing uuid")
		}
		n.Protocol = domain.VMess
		n.Auth = domain.Auth{UUID: p.UUID, AlterID: p.AlterID, Method: strings.ToLower(cmp.Or(p.Cipher, "auto"))}
		n.Transport = clashTransport(p)
		n.PacketEncoding = p.PacketEncoding
		if p.TLS {
			n.TLS = tls
		}

	case "trojan":
		if p.Password == "" {
			return domain.Node{}, fmt.Errorf("clash: trojan missing password")
		}
		n.Protocol = domain.Trojan
		n.Auth = domain.Auth{Password: p.Password}
		n.Transport = clashTransport(p)
		n.TLS = tls

	case "ss", "shadowsocks":
		if p.Cipher == "" || p.Password == "" {
			return domain.Node{}, fmt.Errorf("clash: ss missing cipher/password")
		}
		n.Protocol = domain.SS
		n.Auth = domain.Auth{Method: p.Cipher, Password: p.Password}
		if err := checkPlugin(p.Plugin); err != nil {
			return domain.Node{}, fmt.Errorf("clash: %w", err)
		}
		if p.Plugin == "shadow-tls" && p.PluginOpts != nil {
			n.ShadowTLS = &domain.ShadowTLS{
				Version:  cmp.Or(p.PluginOpts.Version, 3),
				Password: p.PluginOpts.Password,
				SNI:      cmp.Or(p.PluginOpts.Host, p.Server),
			}
		}

	case "hysteria2", "hy2":
		if p.Password == "" {
			return domain.Node{}, fmt.Errorf("clash: hysteria2 missing password")
		}
		n.Protocol = domain.HY2
		n.Auth = domain.Auth{Password: p.Password}
		n.TLS = tls
		n.Obfs, n.ObfsPassword = p.Obfs, p.ObfsPassword

	case "hysteria":
		auth := cmp.Or(p.AuthStr, p.AuthStrOld, p.Password)
		if auth == "" {
			return domain.Node{}, fmt.Errorf("clash: hysteria missing auth")
		}
		n.Protocol = domain.HY1
		n.Auth = domain.Auth{Password: auth}
		n.TLS = tls
		n.ObfsPassword = p.Obfs
		n.UpMbps, n.DownMbps = cmp.Or(int(p.Up), 100), cmp.Or(int(p.Down), 100)

	case "tuic":
		if p.UUID == "" && p.Password == "" {
			return domain.Node{}, fmt.Errorf("clash: tuic missing uuid/password")
		}
		n.Protocol = domain.TUIC
		n.Auth = domain.Auth{UUID: p.UUID, Password: p.Password}
		n.TLS = tls
		if len(n.TLS.ALPN) == 0 {
			n.TLS.ALPN = []string{"h3"}
		}
		n.Congestion = cmp.Or(p.Congestion, "bbr")
		n.UDPRelayMode = cmp.Or(p.UDPRelayMode, "native")

	case "anytls":
		if p.Password == "" {
			return domain.Node{}, fmt.Errorf("clash: anytls missing password")
		}
		n.Protocol = domain.AnyTLS
		n.Auth = domain.Auth{Password: p.Password}
		n.TLS = tls

	case "socks5", "socks":
		n.Protocol = domain.SOCKS
		n.Auth = domain.Auth{Username: p.Username, Password: p.Password}
		if p.TLS {
			n.TLS = tls
		}

	case "http", "https":
		n.Protocol = domain.HTTP
		n.Auth = domain.Auth{Username: p.Username, Password: p.Password}
		if p.TLS || strings.EqualFold(p.Type, "https") {
			n.TLS = tls
		}

	case "wireguard":
		if p.PrivateKey == "" || p.PublicKey == "" {
			return domain.Node{}, fmt.Errorf("clash: wireguard missing keys")
		}
		n.Protocol = domain.WG
		n.WireGuard = &domain.WireGuard{
			PrivateKey:    p.PrivateKey,
			PeerPublicKey: p.PublicKey,
			PreSharedKey:  p.PreSharedKey,
			Address:       addresses(p.IP, p.IPv6),
			Reserved:      p.Reserved,
			MTU:           p.MTU,
		}

	default:
		return domain.Node{}, fmt.Errorf("clash: unsupported type %q", p.Type)
	}
	n.ID = NodeID(n)
	return n, nil
}

type mbps int

func (m *mbps) UnmarshalYAML(n *yaml.Node) error {
	value, _, _ := strings.Cut(strings.TrimSpace(n.Value), " ")
	*m = mbps(atoi(value))
	return nil
}

type reserved []uint8

func (r *reserved) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.SequenceNode {
		var list []int
		if err := n.Decode(&list); err != nil {
			return err
		}
		if len(list) != 3 {
			return fmt.Errorf("reserved: want 3 bytes, got %d", len(list))
		}
		for _, v := range list {
			if v < 0 || v > 255 {
				return fmt.Errorf("reserved: byte %d out of range", v)
			}
			*r = append(*r, uint8(v))
		}
		return nil
	}
	b, err := Unbase64(n.Value)
	if err != nil {
		return err
	}
	if len(b) != 3 {
		return fmt.Errorf("reserved: want 3 bytes, got %d", len(b))
	}
	*r = b
	return nil
}

func addresses(v4, v6 string) []string {
	var out []string
	for _, a := range []string{v4, v6} {
		switch {
		case a == "":
		case strings.Contains(a, "/"):
			out = append(out, a)
		case strings.Contains(a, ":"):
			out = append(out, a+"/128")
		default:
			out = append(out, a+"/32")
		}
	}
	return out
}

func clashTransport(p clashProxy) domain.Transport {
	t := domain.Transport{Network: strings.ToLower(cmp.Or(p.Network, "tcp"))}
	switch t.Network {
	case "ws":
		if p.WSOpts != nil {
			t.Path = p.WSOpts.Path
			for k, v := range p.WSOpts.Headers {
				if strings.EqualFold(k, "Host") {
					t.Host = v
				}
			}
		}
	case "grpc":
		if p.GRPCOpts != nil {
			t.ServiceName = p.GRPCOpts.ServiceName
		}
	case "xhttp", "splithttp":
		t.Network = "xhttp"
		if p.XHTTPOpts != nil {
			t.Path = p.XHTTPOpts.Path
			t.Host = cmp.Or(p.XHTTPOpts.Host, p.XHTTPOpts.Headers["Host"], p.XHTTPOpts.Headers["host"])
			t.Mode = p.XHTTPOpts.Mode
		}
	}
	return t
}
