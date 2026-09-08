package domain

import "time"

type Traffic struct {
	UploadBytes   int64     `yaml:"upload_bytes,omitempty"`
	DownloadBytes int64     `yaml:"download_bytes,omitempty"`
	TotalBytes    int64     `yaml:"total_bytes,omitempty"`
	ExpiresAt     time.Time `yaml:"expires_at,omitempty"`
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
	ID             string     `yaml:"id"`
	Name           string     `yaml:"name,omitempty"`
	Protocol       Proto      `yaml:"protocol"`
	Server         string     `yaml:"server"`
	Port           int        `yaml:"port"`
	Auth           Auth       `yaml:"auth,omitempty"`
	Transport      Transport  `yaml:"transport,omitempty"`
	TLS            *TLS       `yaml:"tls,omitempty"`
	Reality        *Reality   `yaml:"reality,omitempty"`
	Obfs           string     `yaml:"obfs,omitempty"`            // hysteria2
	ObfsPassword   string     `yaml:"obfs_password,omitempty"`   // hysteria2, hysteria xplus
	UpMbps         int        `yaml:"up_mbps,omitempty"`         // hysteria
	DownMbps       int        `yaml:"down_mbps,omitempty"`       // hysteria
	Congestion     string     `yaml:"congestion,omitempty"`      // tuic
	UDPRelayMode   string     `yaml:"udp_relay_mode,omitempty"`  // tuic
	PacketEncoding string     `yaml:"packet_encoding,omitempty"` // vless, vmess: xudp, packetaddr; empty = xudp
	ShadowTLS      *ShadowTLS `yaml:"shadow_tls,omitempty"`
	WireGuard      *WireGuard `yaml:"wireguard,omitempty"`
}

type NodeRef struct {
	SubscriptionID string
	NodeID         string
}

func ValidPort(port int) bool { return port >= 1 && port <= 65535 }

type Auth struct {
	UUID     string `yaml:"uuid,omitempty"`     // vmess, vless, tuic
	Password string `yaml:"password,omitempty"` // trojan, ss, hysteria, anytls, tuic
	Username string `yaml:"username,omitempty"` // socks, http
	Method   string `yaml:"method,omitempty"`   // ss cipher, vmess security
	Flow     string `yaml:"flow,omitempty"`     // vless, e.g. xtls
	AlterID  int    `yaml:"alter_id,omitempty"` // legacy vmess
}

type ShadowTLS struct {
	Version  int    `yaml:"version,omitempty"`
	Password string `yaml:"password,omitempty"`
	SNI      string `yaml:"sni,omitempty"`
}

type WireGuard struct {
	PrivateKey    string   `yaml:"private_key,omitempty"`
	PeerPublicKey string   `yaml:"peer_public_key,omitempty"`
	PreSharedKey  string   `yaml:"pre_shared_key,omitempty"`
	Address       []string `yaml:"address,omitempty"`
	Reserved      []uint8  `yaml:"reserved,omitempty"`
	MTU           uint32   `yaml:"mtu,omitempty"`
}

type Transport struct {
	Network     string `yaml:"network,omitempty"` // tcp, ws, grpc, quic, xhttp
	Path        string `yaml:"path,omitempty"`
	Host        string `yaml:"host,omitempty"`
	ServiceName string `yaml:"service_name,omitempty"` // grpc
	Mode        string `yaml:"mode,omitempty"`         // xhttp: auto, packet-up, stream-up, stream-one
	Extra       string `yaml:"extra,omitempty"`
}

type TLS struct {
	SNI         string   `yaml:"sni,omitempty"`
	ALPN        []string `yaml:"alpn,omitempty"`
	Fingerprint string   `yaml:"fingerprint,omitempty"`
	Insecure    bool     `yaml:"insecure,omitempty"`
}

type Reality struct {
	PublicKey string `yaml:"public_key,omitempty"`
	ShortID   string `yaml:"short_id,omitempty"`
}
