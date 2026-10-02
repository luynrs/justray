package protocols

import (
	"cmp"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"

	"github.com/luynrs/justray/internal/domain"
)

type singboxDoc struct {
	Outbounds []json.RawMessage `json:"outbounds"`
	Endpoints []json.RawMessage `json:"endpoints"`
}

type singboxOutbound struct {
	Type           string        `json:"type"`
	Tag            string        `json:"tag"`
	Server         string        `json:"server"`
	ServerPort     int           `json:"server_port"`
	UUID           string        `json:"uuid"`
	Password       string        `json:"password"`
	Security       string        `json:"security"`
	AlterID        int           `json:"alter_id"`
	Flow           string        `json:"flow"`
	PacketEncoding string        `json:"packet_encoding"`
	Method         string        `json:"method"`
	AuthString     string        `json:"auth_str"`
	Auth           string        `json:"auth"`
	UpMbps         int           `json:"up_mbps"`
	DownMbps       int           `json:"down_mbps"`
	Congestion     string        `json:"congestion_control"`
	UDPRelayMode   string        `json:"udp_relay_mode"`
	Version        int           `json:"version"`
	Username       string        `json:"username"`
	Detour         string        `json:"detour"`
	PrivateKey     string        `json:"private_key"`
	PeerPublicKey  string        `json:"peer_public_key"`
	PreSharedKey   string        `json:"pre_shared_key"`
	LocalAddress   stringOrSlice `json:"local_address"`
	Address        stringOrSlice `json:"address"`
	Reserved       []int         `json:"reserved"`
	MTU            uint32        `json:"mtu"`

	Obfs  json.RawMessage `json:"obfs"`
	Peers []struct {
		Server       string `json:"server"`
		ServerPort   int    `json:"server_port"`
		Address      string `json:"address"`
		Port         int    `json:"port"`
		PublicKey    string `json:"public_key"`
		PreSharedKey string `json:"pre_shared_key"`
		Reserved     []int  `json:"reserved"`
	} `json:"peers"`
	TLS       *singboxTLSConfig       `json:"tls"`
	Transport *singboxTransportConfig `json:"transport"`
}

type singboxTLSConfig struct {
	Enabled    bool          `json:"enabled"`
	ServerName string        `json:"server_name"`
	Insecure   bool          `json:"insecure"`
	ALPN       stringOrSlice `json:"alpn"`
	UTLS       *struct {
		Enabled     bool   `json:"enabled"`
		Fingerprint string `json:"fingerprint"`
	} `json:"utls"`
	Reality *struct {
		Enabled   bool   `json:"enabled"`
		PublicKey string `json:"public_key"`
		ShortID   string `json:"short_id"`
	} `json:"reality"`
}

type singboxTransportConfig struct {
	Type        string                   `json:"type"`
	Path        string                   `json:"path"`
	Headers     map[string]stringOrSlice `json:"headers"`
	Host        stringOrSlice            `json:"host"`
	ServiceName string                   `json:"service_name"`
	Mode        string                   `json:"mode"`
	Extra       string                   `json:"-"`
}

func (transport *singboxTransportConfig) UnmarshalJSON(data []byte) error {
	type config singboxTransportConfig
	if err := json.Unmarshal(data, (*config)(transport)); err != nil {
		return err
	}
	if strings.EqualFold(transport.Type, "xhttp") || strings.EqualFold(transport.Type, "splithttp") {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil {
			return err
		}
		delete(fields, "type")
		if len(transport.Host) > 0 {
			fields["host"], _ = json.Marshal(transport.Host[0])
		}
		data, _ = json.Marshal(fields)
		transport.Extra = string(data)
	}
	return nil
}

var singboxProtos = map[string]domain.Proto{
	"vless": domain.VLESS, "vmess": domain.VMess, "trojan": domain.Trojan,
	"shadowsocks": domain.SS, "hysteria": domain.HY1, "hysteria2": domain.HY2,
	"tuic": domain.TUIC, "anytls": domain.AnyTLS, "wireguard": domain.WG,
	"shadowtls": domain.Shadow, "socks": domain.SOCKS, "http": domain.HTTP,
}

func ParseSingBox(raw []byte) ([]domain.Node, map[string]int, error) {
	if !hasOutboundField(raw, "type") {
		return nil, nil, ErrNotFormat
	}
	var doc singboxDoc
	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &doc); err == nil && (len(doc.Outbounds) > 0 || len(doc.Endpoints) > 0) {
		entries = append(doc.Outbounds, doc.Endpoints...)
	} else if err := json.Unmarshal(raw, &entries); err != nil || len(entries) == 0 {
		return nil, nil, errors.New("sing-box: invalid outbound fields")
	}
	skipped := make(map[string]int)
	var outbounds []singboxOutbound
	for _, entry := range entries {
		var outbound singboxOutbound
		if json.Unmarshal(entry, &outbound) != nil || outbound.Type == "" {
			skipped["sing-box: invalid outbound fields"]++
			continue
		}
		outbounds = append(outbounds, outbound)
	}
	stlsByTag := make(map[string]singboxOutbound)
	detoured := make(map[string]bool)
	for _, ob := range outbounds {
		if strings.EqualFold(ob.Type, "shadowtls") && ob.Tag != "" {
			stlsByTag[ob.Tag] = ob
		}
		if strings.EqualFold(ob.Type, "shadowsocks") && ob.Detour != "" {
			detoured[ob.Detour] = true
		}
	}
	var nodes []domain.Node
	for _, ob := range outbounds {
		if strings.EqualFold(ob.Type, "shadowtls") && detoured[ob.Tag] {
			continue
		}
		switch strings.ToLower(ob.Type) {
		case "direct", "block", "dns", "selector", "urltest":
			continue
		}
		node, err := singboxNode(ob, stlsByTag)
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
		return nil, nil, errors.New("no supported outbounds in sing-box config")
	}
	return nodes, skipped, nil
}

func singboxNode(ob singboxOutbound, stlsByTag map[string]singboxOutbound) (domain.Node, error) {
	proto, ok := singboxProtos[strings.ToLower(ob.Type)]
	if !ok {
		return domain.Node{}, errUnsupported
	}

	server, port := ob.Server, ob.ServerPort
	if proto == domain.WG && len(ob.Peers) > 0 {
		server = cmp.Or(ob.Peers[0].Address, ob.Peers[0].Server)
		port = cmp.Or(ob.Peers[0].Port, ob.Peers[0].ServerPort)
	}
	n := domain.Node{
		Name: cmp.Or(ob.Tag, server), Protocol: proto, Server: server, Port: port,
		PacketEncoding: ob.PacketEncoding,
		Transport:      singboxTransport(ob.Transport),
	}
	n.TLS, n.Reality = singboxTLS(ob.TLS, server)

	switch proto {
	case domain.VLESS:
		n.Auth = domain.Auth{UUID: ob.UUID, Flow: ob.Flow}
	case domain.VMess:
		n.Auth = domain.Auth{UUID: ob.UUID, AlterID: ob.AlterID, Method: cmp.Or(ob.Security, "auto")}
	case domain.Trojan:
		n.Auth = domain.Auth{Password: ob.Password}
		if n.TLS == nil {
			n.TLS = &domain.TLS{SNI: server}
		}
	case domain.SS:
		n.Auth = domain.Auth{Password: ob.Password, Method: ob.Method}
		if ob.Detour != "" {
			stls, ok := stlsByTag[ob.Detour]
			if !ok {
				return domain.Node{}, errors.New("shadowsocks: missing or invalid shadowtls detour")
			}
			n.TLS, n.Reality = singboxTLS(stls.TLS, stls.Server)
			sni := stls.Server
			if stls.TLS != nil && stls.TLS.ServerName != "" {
				sni = stls.TLS.ServerName
			}
			n.Server, n.Port = stls.Server, stls.ServerPort
			n.ShadowTLS = &domain.ShadowTLS{Version: cmp.Or(stls.Version, 3), Password: stls.Password, SNI: sni}
		}
	case domain.HY1:
		auth := ob.AuthString
		if auth == "" && ob.Auth != "" {
			decoded, err := base64.StdEncoding.DecodeString(ob.Auth)
			if err != nil {
				return domain.Node{}, errors.New("hysteria: invalid base64 auth")
			}
			auth = string(decoded)
		}
		n.Auth = domain.Auth{Password: cmp.Or(auth, ob.Password)}
		n.UpMbps, n.DownMbps = ob.UpMbps, ob.DownMbps
		if len(ob.Obfs) > 0 && json.Unmarshal(ob.Obfs, &n.ObfsPassword) != nil {
			return domain.Node{}, errors.New("sing-box: invalid outbound fields")
		}
	case domain.HY2:
		n.Auth = domain.Auth{Password: cmp.Or(ob.Password, ob.Auth)}
		var obfs struct {
			Type     string `json:"type"`
			Password string `json:"password"`
		}
		if len(ob.Obfs) > 0 && json.Unmarshal(ob.Obfs, &obfs) != nil {
			return domain.Node{}, errors.New("sing-box: invalid outbound fields")
		}
		n.UpMbps, n.DownMbps, n.Obfs, n.ObfsPassword = ob.UpMbps, ob.DownMbps, obfs.Type, obfs.Password
	case domain.TUIC:
		n.Auth = domain.Auth{UUID: ob.UUID, Password: ob.Password}
		n.Congestion, n.UDPRelayMode = ob.Congestion, ob.UDPRelayMode
	case domain.AnyTLS:
		n.Auth = domain.Auth{Password: ob.Password}
	case domain.WG:
		peerPK, psk, res := ob.PeerPublicKey, ob.PreSharedKey, ob.Reserved
		if len(ob.Peers) > 0 {
			peerPK, psk = ob.Peers[0].PublicKey, ob.Peers[0].PreSharedKey
			if len(ob.Peers[0].Reserved) > 0 {
				res = ob.Peers[0].Reserved
			}
		}
		var reserved []uint8
		for _, b := range res {
			if b < 0 || b > 255 {
				return domain.Node{}, errors.New("wireguard: reserved byte out of range")
			}
			reserved = append(reserved, uint8(b))
		}
		addrs := fixCIDRs(append(append([]string(nil), ob.LocalAddress...), ob.Address...))
		if ob.PrivateKey != "" && len(addrs) > 0 {
			n.WireGuard = &domain.WireGuard{
				PrivateKey: ob.PrivateKey, PeerPublicKey: peerPK, PreSharedKey: psk,
				Address: addrs, Reserved: reserved, MTU: ob.MTU,
			}
		}
	case domain.Shadow:
		sni := server
		if ob.TLS != nil && ob.TLS.ServerName != "" {
			sni = ob.TLS.ServerName
		}
		n.ShadowTLS = &domain.ShadowTLS{Version: cmp.Or(ob.Version, 3), Password: ob.Password, SNI: sni}
	case domain.SOCKS, domain.HTTP:
		n.Auth = domain.Auth{Username: ob.Username, Password: ob.Password}
	}

	return n, nil
}

func singboxTransport(t *singboxTransportConfig) domain.Transport {
	if t == nil {
		return domain.Transport{Network: "tcp"}
	}
	network := cmp.Or(strings.ToLower(t.Type), "tcp")
	if network == "xhttp" || network == "splithttp" {
		return domain.Transport{Network: "xhttp", Extra: t.Extra}
	}
	var host string
	if len(t.Host) > 0 {
		host = t.Host[0]
	} else {
		for name, values := range t.Headers {
			if strings.EqualFold(name, "Host") && len(values) > 0 {
				host = values[0]
			}
		}
	}
	return domain.Transport{Network: network, Path: t.Path, Host: host, ServiceName: t.ServiceName, Mode: t.Mode}
}

func singboxTLS(t *singboxTLSConfig, server string) (*domain.TLS, *domain.Reality) {
	if t == nil || !t.Enabled {
		return nil, nil
	}
	var reality *domain.Reality
	var fp string
	if t.UTLS != nil && t.UTLS.Enabled {
		fp = t.UTLS.Fingerprint
	}
	if t.Reality != nil && t.Reality.Enabled {
		reality = &domain.Reality{PublicKey: t.Reality.PublicKey, ShortID: t.Reality.ShortID}
	}
	fp, insecure := cleanFingerprint(fp, t.Insecure)
	return &domain.TLS{SNI: cmp.Or(t.ServerName, server), ALPN: t.ALPN, Fingerprint: fp, Insecure: insecure}, reality
}
