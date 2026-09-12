package domain

import "time"

type Traffic struct {
	UploadBytes   int64     `json:"upload_bytes,omitempty" yaml:"upload_bytes,omitempty"`
	DownloadBytes int64     `json:"download_bytes,omitempty" yaml:"download_bytes,omitempty"`
	TotalBytes    int64     `json:"total_bytes,omitempty" yaml:"total_bytes,omitempty"`
	ExpiresAt     time.Time `json:"expires_at,omitempty" yaml:"expires_at,omitempty"`
}

type Proto string

const (
	VMess  Proto = "vmess"
	VLess  Proto = "vless"
	Trojan Proto = "trojan"
	SS     Proto = "shadowsocks"
	HY1    Proto = "hysteria"
	HY2    Proto = "hysteria2"
	TUIC   Proto = "tuic"
	AnyTLS Proto = "anytls"
	SOCKS  Proto = "socks"
	HTTP   Proto = "http"
	WG     Proto = "wireguard"
	Shadow Proto = "shadowtls"
)

type Node struct {
	ID             string     `json:"id" yaml:"id"`
	Name           string     `json:"name,omitempty" yaml:"name,omitempty"`
	Protocol       Proto      `json:"protocol" yaml:"protocol"`
	Server         string     `json:"server" yaml:"server"`
	Port           int        `json:"port" yaml:"port"`
	Auth           Auth       `json:"auth,omitempty" yaml:"auth,omitempty"`
	Transport      Transport  `json:"transport,omitempty" yaml:"transport,omitempty"`
	TLS            *TLS       `json:"tls,omitempty" yaml:"tls,omitempty"`
	Reality        *Reality   `json:"reality,omitempty" yaml:"reality,omitempty"`
	Obfs           string     `json:"obfs,omitempty" yaml:"obfs,omitempty"`                       // hysteria2
	ObfsPassword   string     `json:"obfs_password,omitempty" yaml:"obfs_password,omitempty"`     // hysteria2, hysteria xplus
	UpMbps         int        `json:"up_mbps,omitempty" yaml:"up_mbps,omitempty"`                 // hysteria
	DownMbps       int        `json:"down_mbps,omitempty" yaml:"down_mbps,omitempty"`             // hysteria
	Congestion     string     `json:"congestion,omitempty" yaml:"congestion,omitempty"`           // tuic
	UDPRelayMode   string     `json:"udp_relay_mode,omitempty" yaml:"udp_relay_mode,omitempty"`   // tuic
	PacketEncoding string     `json:"packet_encoding,omitempty" yaml:"packet_encoding,omitempty"` // vless, vmess: xudp, packetaddr; empty = xudp
	ShadowTLS      *ShadowTLS `json:"shadow_tls,omitempty" yaml:"shadow_tls,omitempty"`
	WireGuard      *WireGuard `json:"wireguard,omitempty" yaml:"wireguard,omitempty"`
}

type NodeRef struct {
	SubscriptionID string
	NodeID         string
}

func ValidPort(port int) bool { return port >= 1 && port <= 65535 }

type Auth struct {
	UUID     string `json:"uuid,omitempty" yaml:"uuid,omitempty"`         // vmess, vless, tuic
	Password string `json:"password,omitempty" yaml:"password,omitempty"` // trojan, ss, hysteria, anytls, tuic
	Username string `json:"username,omitempty" yaml:"username,omitempty"` // socks, http
	Method   string `json:"method,omitempty" yaml:"method,omitempty"`     // ss cipher, vmess security
	Flow     string `json:"flow,omitempty" yaml:"flow,omitempty"`         // vless, e.g. xtls
	AlterID  int    `json:"alter_id,omitempty" yaml:"alter_id,omitempty"` // legacy vmess
}

type ShadowTLS struct {
	Version  int    `json:"version,omitempty" yaml:"version,omitempty"`
	Password string `json:"password,omitempty" yaml:"password,omitempty"`
	SNI      string `json:"sni,omitempty" yaml:"sni,omitempty"`
}

type WireGuard struct {
	PrivateKey    string   `json:"private_key,omitempty" yaml:"private_key,omitempty"`
	PeerPublicKey string   `json:"peer_public_key,omitempty" yaml:"peer_public_key,omitempty"`
	PreSharedKey  string   `json:"pre_shared_key,omitempty" yaml:"pre_shared_key,omitempty"`
	Address       []string `json:"address,omitempty" yaml:"address,omitempty"`
	Reserved      []uint8  `json:"reserved,omitempty" yaml:"reserved,omitempty"`
	MTU           uint32   `json:"mtu,omitempty" yaml:"mtu,omitempty"`
}

type Transport struct {
	Network     string `json:"network,omitempty" yaml:"network,omitempty"` // tcp, ws, grpc, quic, xhttp
	Path        string `json:"path,omitempty" yaml:"path,omitempty"`
	Host        string `json:"host,omitempty" yaml:"host,omitempty"`
	ServiceName string `json:"service_name,omitempty" yaml:"service_name,omitempty"` // grpc
	Mode        string `json:"mode,omitempty" yaml:"mode,omitempty"`                 // xhttp: auto, packet-up, stream-up, stream-one
	Extra       string `json:"extra,omitempty" yaml:"extra,omitempty"`
}

type TLS struct {
	SNI         string   `json:"sni,omitempty" yaml:"sni,omitempty"`
	ALPN        []string `json:"alpn,omitempty" yaml:"alpn,omitempty"`
	Fingerprint string   `json:"fingerprint,omitempty" yaml:"fingerprint,omitempty"`
	Insecure    bool     `json:"insecure,omitempty" yaml:"insecure,omitempty"`
}

type Reality struct {
	PublicKey string `json:"public_key,omitempty" yaml:"public_key,omitempty"`
	ShortID   string `json:"short_id,omitempty" yaml:"short_id,omitempty"`
}
