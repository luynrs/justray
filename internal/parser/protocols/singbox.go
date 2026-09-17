package protocols

import (
	"cmp"
	"encoding/json"
	"errors"
	"strings"

	"github.com/luynrs/justray/internal/domain"
)

type singboxDoc struct {
	Outbounds []singboxOutbound `json:"outbounds"`
	Endpoints []singboxOutbound `json:"endpoints"`
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

	Obfs struct {
		Type     string `json:"type"`
		Password string `json:"password"`
	} `json:"obfs"`
	Peers []struct {
		Server       string `json:"server"`
		ServerPort   int    `json:"server_port"`
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
	Type        string            `json:"type"`
	Path        string            `json:"path"`
	Headers     map[string]string `json:"headers"`
	Host        stringOrSlice     `json:"host"`
	ServiceName string            `json:"service_name"`
	Mode        string            `json:"mode"`
}


var singboxProtos = map[string]domain.Proto{
	"vless": domain.VLess, "vmess": domain.VMess, "trojan": domain.Trojan,
	"shadowsocks": domain.SS, "hysteria": domain.HY1, "hysteria2": domain.HY2,
	"tuic": domain.TUIC, "anytls": domain.AnyTLS, "wireguard": domain.WG,
	"shadowtls": domain.Shadow, "socks": domain.SOCKS, "http": domain.HTTP,
}

func ParseSingBox(raw []byte) ([]domain.Node, error) {
	var doc singboxDoc
	var outbounds []singboxOutbound
	if err := json.Unmarshal(raw, &doc); err == nil && (len(doc.Outbounds) > 0 || len(doc.Endpoints) > 0) {
		outbounds = append(doc.Outbounds, doc.Endpoints...)
	} else if err := json.Unmarshal(raw, &outbounds); err != nil || len(outbounds) == 0 {
		return nil, errors.New("not a sing-box config")
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
		node, err := parseSingBoxOutbound(ob, stlsByTag)
		if err != nil {
			continue
		}
		node.ID = NodeID(node)
		nodes = append(nodes, node)
	}
	if len(nodes) == 0 {
		return nil, errors.New("no supported outbounds in sing-box config")
	}
	return nodes, nil
}

func parseSingBoxOutbound(ob singboxOutbound, stlsByTag map[string]singboxOutbound) (domain.Node, error) {
	proto, ok := singboxProtos[strings.ToLower(ob.Type)]
	if !ok {
		return domain.Node{}, errors.New("unsupported sing-box outbound")
	}

	server, port := ob.Server, ob.ServerPort
	if proto == domain.WG && len(ob.Peers) > 0 {
		server = cmp.Or(server, ob.Peers[0].Server)
		port = cmp.Or(port, ob.Peers[0].ServerPort)
	}
	if server == "" || !domain.ValidPort(port) {
		return domain.Node{}, errors.New("missing server or port")
	}

	n := domain.Node{
		Name: cmp.Or(ob.Tag, server), Protocol: proto, Server: server, Port: port,
		PacketEncoding: ob.PacketEncoding,
		Transport:      singboxTransport(ob.Transport),
	}
	n.TLS, n.Reality = singboxTLS(ob.TLS, server)

	switch proto {
	case domain.VLess:
		n.Auth = domain.Auth{UUID: ob.UUID, Flow: ob.Flow}
	case domain.VMess:
		n.Auth = domain.Auth{UUID: ob.UUID, Method: cmp.Or(ob.Security, "auto"), AlterID: ob.AlterID}
	case domain.Trojan:
		n.Auth = domain.Auth{Password: ob.Password}
		if n.TLS == nil {
			n.TLS = &domain.TLS{SNI: server}
		}
	case domain.SS:
		n.Auth = domain.Auth{Password: ob.Password, Method: ob.Method}
		if stls, ok := stlsByTag[ob.Detour]; ok {
			sni := server
			if stls.TLS != nil && stls.TLS.ServerName != "" {
				sni = stls.TLS.ServerName
			}
			n.Server, n.Port = stls.Server, stls.ServerPort
			n.ShadowTLS = &domain.ShadowTLS{Version: cmp.Or(stls.Version, 3), Password: stls.Password, SNI: sni}
		}
	case domain.HY1:
		n.Auth = domain.Auth{Password: cmp.Or(ob.AuthString, ob.Auth, ob.Password)}
		n.UpMbps, n.DownMbps, n.ObfsPassword = ob.UpMbps, ob.DownMbps, ob.Obfs.Password
	case domain.HY2:
		n.Auth = domain.Auth{Password: cmp.Or(ob.Password, ob.Auth)}
		n.UpMbps, n.DownMbps, n.Obfs, n.ObfsPassword = ob.UpMbps, ob.DownMbps, ob.Obfs.Type, ob.Obfs.Password
	case domain.TUIC:
		n.Auth = domain.Auth{UUID: ob.UUID, Password: ob.Password}
		n.Congestion, n.UDPRelayMode = ob.Congestion, ob.UDPRelayMode
	case domain.AnyTLS:
		n.Auth = domain.Auth{Password: ob.Password}
	case domain.WG:
		peerPK, psk := ob.PeerPublicKey, ob.PreSharedKey
		res := ob.Reserved
		if len(ob.Peers) > 0 {
			peerPK = cmp.Or(peerPK, ob.Peers[0].PublicKey)
			psk = cmp.Or(psk, ob.Peers[0].PreSharedKey)
			if len(res) == 0 {
				res = ob.Peers[0].Reserved
			}
		}
		var reserved []uint8
		for _, b := range res {
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

	if (proto == domain.VLess || proto == domain.VMess || proto == domain.TUIC) && n.Auth.UUID == "" {
		return domain.Node{}, errors.New("missing uuid")
	}
	if (proto == domain.Trojan || proto == domain.SS || proto == domain.HY2 || proto == domain.AnyTLS || proto == domain.Shadow) && n.Auth.Password == "" && (n.ShadowTLS == nil || n.ShadowTLS.Password == "") {
		return domain.Node{}, errors.New("missing password")
	}
	if proto == domain.SS && n.Auth.Method == "" {
		return domain.Node{}, errors.New("missing method")
	}
	if proto == domain.WG && n.WireGuard == nil {
		return domain.Node{}, errors.New("missing wireguard settings")
	}
	return n, nil
}

func singboxTransport(t *singboxTransportConfig) domain.Transport {
	if t == nil {
		return domain.Transport{Network: "tcp"}
	}
	net := strings.ToLower(t.Type)
	if net == "" {
		net = "tcp"
	}
	var host string
	if len(t.Host) > 0 {
		host = t.Host[0]
	} else if t.Headers != nil {
		host = cmp.Or(t.Headers["Host"], t.Headers["host"])
	}
	return domain.Transport{Network: net, Path: t.Path, Host: host, ServiceName: t.ServiceName, Mode: t.Mode}
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
