package protocols

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/luynrs/justray/internal/domain"
)

type clashProxy struct {
	Name                string   `yaml:"name"`
	Type                string   `yaml:"type"`
	Server              string   `yaml:"server"`
	Port                int      `yaml:"port"`
	UUID                string   `yaml:"uuid"`
	AlterID             int      `yaml:"alterId"`
	Password            string   `yaml:"password"`
	Cipher              string   `yaml:"cipher"`
	Network             string   `yaml:"network"`
	TLS                 bool     `yaml:"tls"`
	SkipCertVerify      bool     `yaml:"skip-cert-verify"`
	SkipCertVerifySnake bool     `yaml:"skip_cert_verify"`
	ServerName          string   `yaml:"servername"`
	ServerNameKebab     string   `yaml:"server-name"`
	ServerNameSnake     string   `yaml:"server_name"`
	SNI                 string   `yaml:"sni"`
	Flow                string   `yaml:"flow"`
	ClientFingerprint   string   `yaml:"client-fingerprint"`
	Fingerprint         string   `yaml:"fingerprint"`
	ALPN                []string `yaml:"alpn"`
	Obfs                string   `yaml:"obfs"`
	ObfsPassword        string   `yaml:"obfs-password"`
	ObfsParam           string   `yaml:"obfs-param"`
	Username            string   `yaml:"username"`
	AuthStr             string   `yaml:"auth-str"`
	Version             int      `yaml:"version"`
	Up                  mbps     `yaml:"up"`
	Down                mbps     `yaml:"down"`
	Congestion          string   `yaml:"congestion-controller"`
	UDPRelayMode        string   `yaml:"udp-relay-mode"`
	PacketEncoding      string   `yaml:"packet-encoding"`

	PrivateKey   string   `yaml:"private-key"`
	PublicKey    string   `yaml:"public-key"`
	PreSharedKey string   `yaml:"pre-shared-key"`
	IP           string   `yaml:"ip"`
	IPv6         string   `yaml:"ipv6"`
	Address      string   `yaml:"address"`
	Addresses    []string `yaml:"addresses"`
	MTU          uint32   `yaml:"mtu"`
	Reserved     reserved `yaml:"reserved"`

	Plugin        string              `yaml:"plugin"`
	PluginOpts    *clashShadowTLSOpts `yaml:"plugin-opts"`
	ShadowTLSOpts *clashShadowTLSOpts `yaml:"shadow-tls-opts"`

	WSOpts *struct {
		Path    string            `yaml:"path"`
		Headers map[string]string `yaml:"headers"`
	} `yaml:"ws-opts"`
	GRPCOpts *struct {
		ServiceName      string `yaml:"grpc-service-name"`
		ServiceNameKebab string `yaml:"service-name"`
	} `yaml:"grpc-opts"`
	XHTTPOpts   xhttpYAML `yaml:"xhttp-opts"`
	RealityOpts *struct {
		PublicKey      string `yaml:"public-key"`
		PublicKeyCamel string `yaml:"publicKey"`
		PublicKeySnake string `yaml:"public_key"`
		ShortID        string `yaml:"short-id"`
		ShortIDCamel   string `yaml:"shortId"`
		ShortIDSnake   string `yaml:"short_id"`
		Fingerprint    string `yaml:"fingerprint"`
	} `yaml:"reality-opts"`
}

type xhttpYAML string

func (settings *xhttpYAML) UnmarshalYAML(node *yaml.Node) error {
	var fields map[string]any
	if err := node.Decode(&fields); err != nil {
		return err
	}
	data, err := json.Marshal(fields)
	*settings = xhttpYAML(data)
	return err
}

// Clash/Mihomo "proxies:" list
func ParseClash(raw []byte) ([]domain.Node, map[string]int, error) {
	var doc struct {
		Proxies []yaml.Node `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		for line := range strings.Lines(string(raw)) {
			if strings.HasPrefix(line, "proxies:") {
				return nil, nil, fmt.Errorf("clash: invalid YAML")
			}
		}
		return nil, nil, ErrNotFormat
	}
	if doc.Proxies == nil {
		return nil, nil, ErrNotFormat
	}

	var nodes []domain.Node
	skipped := make(map[string]int)
	for _, raw := range doc.Proxies {
		var p clashProxy
		if err := raw.Decode(&p); err != nil || p.Type == "" {
			skipped["clash: invalid proxy fields"]++
			continue
		}
		node, err := clashNode(p)
		if errors.Is(err, errUnsupported) {
			skipped["unsupported"]++
			continue
		}
		if err != nil {
			skipped[err.Error()]++
			continue
		}
		nodes = append(nodes, node)
	}
	if len(nodes) == 0 && len(skipped) == 0 {
		return nil, nil, fmt.Errorf("clash: no supported proxies")
	}
	return nodes, skipped, nil
}

func clashNode(p clashProxy) (domain.Node, error) {
	n := domain.Node{
		Name:   cmp.Or(p.Name, p.Server),
		Server: p.Server,
		Port:   p.Port,
	}
	fp, insecure := cleanFingerprint(p.Fingerprint, p.SkipCertVerify || p.SkipCertVerifySnake)
	clientFP := cmp.Or(p.ClientFingerprint, fp)
	tls := &domain.TLS{
		SNI:         cmp.Or(p.SNI, p.ServerName, p.ServerNameKebab, p.ServerNameSnake, p.Server),
		ALPN:        p.ALPN,
		Fingerprint: clientFP,
		Insecure:    insecure,
	}

	switch strings.ToLower(p.Type) {
	case "vless":
		n.Protocol = domain.VLESS
		n.Auth = domain.Auth{UUID: p.UUID, Flow: p.Flow}
		n.Transport = clashTransport(p)
		n.PacketEncoding = p.PacketEncoding
		if p.TLS || p.RealityOpts != nil {
			n.TLS = tls
		}
		if p.RealityOpts != nil {
			if clientFP == "" && p.RealityOpts.Fingerprint != "" {
				tls.Fingerprint = p.RealityOpts.Fingerprint
			}
			n.Reality = &domain.Reality{
				PublicKey: cmp.Or(p.RealityOpts.PublicKey, p.RealityOpts.PublicKeyCamel, p.RealityOpts.PublicKeySnake),
				ShortID:   cmp.Or(p.RealityOpts.ShortID, p.RealityOpts.ShortIDCamel, p.RealityOpts.ShortIDSnake),
			}
		}

	case "vmess":
		n.Protocol = domain.VMess
		n.Auth = domain.Auth{UUID: p.UUID, AlterID: p.AlterID, Method: strings.ToLower(cmp.Or(p.Cipher, "auto"))}
		n.Transport = clashTransport(p)
		n.PacketEncoding = p.PacketEncoding
		if p.TLS {
			n.TLS = tls
		}

	case "trojan":
		n.Protocol = domain.Trojan
		n.Auth = domain.Auth{Password: p.Password}
		n.Transport = clashTransport(p)
		n.TLS = tls

	case "ss", "shadowsocks":
		n.Protocol = domain.SS
		n.Auth = domain.Auth{Method: p.Cipher, Password: p.Password}
		if plugin, _, _ := strings.Cut(p.Plugin, ";"); plugin != "" && plugin != "shadow-tls" {
			return domain.Node{}, fmt.Errorf("clash: unsupported plugin %q", plugin)
		}
		stlsOpts := p.PluginOpts
		if stlsOpts == nil {
			stlsOpts = p.ShadowTLSOpts
		}
		if p.Plugin == "shadow-tls" || p.ShadowTLSOpts != nil {
			if stlsOpts == nil {
				return domain.Node{}, fmt.Errorf("shadowtls: missing settings")
			}
			n.ShadowTLS = &domain.ShadowTLS{
				Version:  cmp.Or(stlsOpts.Version, 3),
				Password: stlsOpts.Password,
				SNI:      cmp.Or(stlsOpts.Host, stlsOpts.SNI, p.Server),
			}
			n.TLS = tls
			n.TLS.SNI = n.ShadowTLS.SNI
		}

	case "shadow-tls", "shadowtls", "stls":
		pw := cmp.Or(p.Password, p.AuthStr)
		n.Protocol = domain.Shadow
		n.TLS = tls
		n.ShadowTLS = &domain.ShadowTLS{
			Version:  cmp.Or(p.Version, 3),
			Password: pw,
			SNI:      cmp.Or(p.SNI, p.ServerName, p.ServerNameKebab, p.ServerNameSnake, p.Server),
		}

	case "hysteria2", "hy2":
		n.Protocol = domain.HY2
		n.Auth = domain.Auth{Password: p.Password}
		n.TLS = tls
		n.Obfs = p.Obfs
		n.ObfsPassword = cmp.Or(p.ObfsPassword, p.ObfsParam)
		if n.Obfs == "" && n.ObfsPassword != "" {
			n.Obfs = "salamander"
		}

	case "hysteria", "hy", "hy1":
		auth := cmp.Or(p.AuthStr, p.Password)
		if auth == "" {
			return domain.Node{}, fmt.Errorf("clash: hysteria missing auth")
		}
		n.Protocol = domain.HY1
		n.Auth = domain.Auth{Password: auth}
		n.TLS = tls
		n.ObfsPassword = cmp.Or(p.ObfsPassword, p.Obfs)
		n.UpMbps, n.DownMbps = cmp.Or(int(p.Up), 100), cmp.Or(int(p.Down), 100)

	case "tuic", "tuic5", "tuic-v5", "tuicv5":
		n.Protocol = domain.TUIC
		n.Auth = domain.Auth{UUID: p.UUID, Password: p.Password}
		n.TLS = tls
		if len(n.TLS.ALPN) == 0 {
			n.TLS.ALPN = []string{"h3"}
		}
		n.Congestion = cmp.Or(p.Congestion, "bbr")
		n.UDPRelayMode = cmp.Or(p.UDPRelayMode, "native")

	case "anytls":
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

	case "wireguard", "wg":
		addr := addresses(cmp.Or(p.IP, p.Address), p.IPv6)
		if len(addr) == 0 {
			for _, a := range p.Addresses {
				addr = append(addr, addresses(a, "")...)
			}
		}
		n.Protocol = domain.WG
		n.WireGuard = &domain.WireGuard{
			PrivateKey:    p.PrivateKey,
			PeerPublicKey: p.PublicKey,
			PreSharedKey:  p.PreSharedKey,
			Address:       addr,
			Reserved:      p.Reserved,
			MTU:           p.MTU,
		}

	default:
		return domain.Node{}, errUnsupported
	}
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

type clashShadowTLSOpts struct {
	Host     string `yaml:"host"`
	SNI      string `yaml:"sni"`
	Password string `yaml:"password"`
	Version  int    `yaml:"version"`
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
			t.ServiceName = cmp.Or(p.GRPCOpts.ServiceName, p.GRPCOpts.ServiceNameKebab)
		}
	case "xhttp", "splithttp":
		t.Network, t.Extra = "xhttp", string(p.XHTTPOpts)
	}
	return t
}
