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
	ID             string
	Name           string
	Protocol       Proto
	Server         string
	Port           int
	Auth           Auth
	Transport      Transport
	TLS            *TLS // nil means plaintext
	Reality        *Reality
	Obfs           string // hysteria2
	ObfsPassword   string // hysteria2, hysteria xplus
	UpMbps         int    // hysteria
	DownMbps       int    // hysteria
	Congestion     string // tuic
	UDPRelayMode   string // tuic
	PacketEncoding string // vless, vmess: xudp, packetaddr; empty = xudp
	ShadowTLS      *ShadowTLS
	WireGuard      *WireGuard
}

type NodeRef struct {
	SubscriptionID string
	NodeID         string
}

func ValidPort(port int) bool { return port >= 1 && port <= 65535 }

type Auth struct {
	UUID     string // vmess, vless, tuic
	Password string // trojan, ss, hysteria, anytls, tuic
	Username string // socks, http
	Method   string // ss cipher, vmess security
	Flow     string // vless, e.g. xtls
	AlterID  int    // legacy vmess
}

type ShadowTLS struct {
	Version  int
	Password string
	SNI      string
}

type WireGuard struct {
	PrivateKey    string
	PeerPublicKey string
	PreSharedKey  string
	Address       []string
	Reserved      []uint8
	MTU           uint32
}

type Transport struct {
	Network     string // tcp, ws, grpc, quic
	Path        string
	Host        string
	ServiceName string // grpc
}

type TLS struct {
	SNI         string
	ALPN        []string
	Fingerprint string
	Insecure    bool
}

type Reality struct {
	PublicKey string
	ShortID   string
}
