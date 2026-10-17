package parser

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/sagernet/sing-box/option"
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

	AmneziaWG                   map[string]any   `yaml:"amnezia-wg-option"`
	PersistentKeepaliveInterval *option.AWGRange `yaml:"persistent-keepalive"`

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
func (result *parseResult) readClash(entries []yaml.Node) {
	for _, entry := range entries {
		var proxy clashProxy
		if entry.Decode(&proxy) != nil || proxy.Type == "" {
			result.add(domain.Node{}, errors.New("clash: invalid proxy fields"))
			continue
		}
		result.add(clashNode(proxy))
	}
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
				return domain.Node{}, errors.New("shadowtls: missing settings")
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
		n.UpMbps, n.DownMbps = int(p.Up), int(p.Down)
		n.TLS = tls
		n.Obfs = p.Obfs
		n.ObfsPassword = cmp.Or(p.ObfsPassword, p.ObfsParam)
		if n.Obfs == "" && n.ObfsPassword != "" {
			n.Obfs = "salamander"
		}

	case "hysteria", "hy", "hy1":
		auth := cmp.Or(p.AuthStr, p.Password)
		if auth == "" {
			return domain.Node{}, errors.New("clash: hysteria missing auth")
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
		amnezia, err := parseAmneziaWG(p.AmneziaWG)
		if err != nil {
			return domain.Node{}, err
		}
		addr := parseAddresses(cmp.Or(p.IP, p.Address), p.IPv6)
		if len(addr) == 0 {
			for _, a := range p.Addresses {
				addr = append(addr, parseAddresses(a, "")...)
			}
		}
		n.Protocol = domain.WG
		n.WireGuard = &domain.WireGuard{
			AmneziaWGOptions:            amnezia,
			PersistentKeepaliveInterval: p.PersistentKeepaliveInterval,
			PrivateKey:                  p.PrivateKey,
			PeerPublicKey:               p.PublicKey,
			PreSharedKey:                p.PreSharedKey,
			Address:                     addr,
			Reserved:                    p.Reserved,
			MTU:                         p.MTU,
		}

	default:
		return domain.Node{}, errUnsupported
	}
	return n, nil
}

type mbps int

func (bandwidth *mbps) UnmarshalYAML(node *yaml.Node) error {
	value, _, _ := strings.Cut(strings.TrimSpace(node.Value), " ")
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return errors.New("invalid bandwidth")
	}
	*bandwidth = mbps(parsed)
	return nil
}

type reserved []uint8

func (value *reserved) UnmarshalYAML(node *yaml.Node) error {
	var data []uint8
	var err error
	if node.Kind == yaml.SequenceNode {
		err = node.Decode(&data)
	} else {
		data, err = decodeBase64(node.Value)
	}
	if err != nil {
		return err
	}
	if len(data) != 3 {
		return fmt.Errorf("reserved: want 3 bytes, got %d", len(data))
	}
	*value = data
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
