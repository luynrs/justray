package parser

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/luynrs/justray/internal/shared/domain"
)

func vmessURI(t *testing.T) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"ps": "node", "add": "example.com", "port": "443",
		"id":  "11111111-1111-1111-1111-111111111111",
		"net": "ws", "path": "/ray", "tls": "tls", "sni": "example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	return "vmess://" + base64.StdEncoding.EncodeToString(raw) + "#node"
}

func TestParseURI(t *testing.T) {
	cases := map[string]struct {
		uri      string
		protocol domain.Proto
		server   string
		port     int
	}{
		"vless":       {"vless://11111111-1111-1111-1111-111111111111@example.com:443?security=tls&sni=example.com#node", domain.VLess, "example.com", 443},
		"trojan":      {"trojan://secret@example.com:443#node", domain.Trojan, "example.com", 443},
		"shadowsocks": {"ss://YWVzLTI1Ni1nY206cGFzcw==@example.com:8388#node", domain.SS, "example.com", 8388},
		"hysteria":    {"hysteria://example.com:443?auth=secret&upmbps=50&downmbps=200#node", domain.HY1, "example.com", 443},
		"hysteria2":   {"hysteria2://user:pass@example.com:443#node", domain.HY2, "example.com", 443},
		"hy2 alias":   {"hy2://user:pass@example.com:443#node", domain.HY2, "example.com", 443},
		"tuic":        {"tuic://11111111-1111-1111-1111-111111111111:pass@example.com:443#node", domain.TUIC, "example.com", 443},
		"anytls":      {"anytls://secret@example.com:443#node", domain.AnyTLS, "example.com", 443},
		"socks5":      {"socks5://user:pass@example.com:1080#node", domain.SOCKS, "example.com", 1080},
		"socks alias": {"socks://user:pass@example.com:1080#node", domain.SOCKS, "example.com", 1080},
		"wireguard":   {"wireguard://priv%2Fkey@example.com:51820?publickey=pub%2Bkey&address=10.0.0.2%2F32#node", domain.WG, "example.com", 51820},
		"wg alias":    {"wg://priv@example.com:51820?publickey=pub&address=10.0.0.2%2F32#node", domain.WG, "example.com", 51820},
		"shadowtls":   {"shadowtls://:secret@example.com:443?version=3&sni=cloud.example#node", domain.Shadow, "example.com", 443},
		"stls alias":  {"shadow-tls://secret@example.com:443#node", domain.Shadow, "example.com", 443},
		"vmess":       {vmessURI(t), domain.VMess, "example.com", 443},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if !IsLink(c.uri) {
				t.Fatalf("IsLink(%.20q) = false", c.uri)
			}
			n, err := ParseURI(c.uri)
			if err != nil {
				t.Fatalf("ParseURI: %v", err)
			}
			if n.Protocol != c.protocol || n.Server != c.server || n.Port != c.port {
				t.Fatalf("got %s %s:%d, want %s %s:%d", n.Protocol, n.Server, n.Port, c.protocol, c.server, c.port)
			}
			if len(n.ID) != 16 {
				t.Fatalf("node ID %q has length %d, want 16", n.ID, len(n.ID))
			}
			if c.protocol == domain.WG && (n.WireGuard == nil || len(n.WireGuard.Address) == 0) {
				t.Fatal("missing WireGuard settings")
			}
		})
	}
}

func TestParseShadowsocksShadowTLS(t *testing.T) {
	uri := "ss://YWVzLTI1Ni1nY206cGFzcw==@example.com:8388?plugin=shadow-tls%3Bhost%3Dcloud.example%3Bpassword%3Dsecret%3Bversion%3D2#node"
	n, err := ParseURI(uri)
	if err != nil {
		t.Fatal(err)
	}
	if n.ShadowTLS == nil || n.ShadowTLS.Version != 2 || n.ShadowTLS.SNI != "cloud.example" {
		t.Fatalf("got %#v, want ShadowTLS v2", n.ShadowTLS)
	}
}

func TestParseURIRejects(t *testing.T) {
	bad := []string{
		"",
		"not a uri at all",
		"http://example.com",
		"vless://example.com:443",           // no uuid
		"trojan://example.com",              // no host:port
		"ss://not-base64-and-no-at@x:1",     // undecodable
		"vmess://not-base64-json",           // undecodable
		"hysteria2://user:pass@example.com", // missing port
	}
	for _, uri := range bad {
		if _, err := ParseURI(uri); err == nil {
			t.Errorf("ParseURI(%.40q): want error, got none", uri)
		}
	}
}

func TestParseSubscriptionPlainList(t *testing.T) {
	body := strings.Join([]string{
		"# a comment line",
		"",
		"trojan://secret@example.com:443#one",
		"vless://11111111-1111-1111-1111-111111111111@example.org:8443?security=tls#two",
		"shit shit shit shit shit",
	}, "\n")

	nodes, err := ParseSubscription([]byte(body))
	if err != nil {
		t.Fatalf("ParseSubscription: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("got %d nodes, want 2", len(nodes))
	}
}

func TestParseSubscriptionBase64Wrapped(t *testing.T) {
	body := "trojan://secret@example.com:443#one\nvless://11111111-1111-1111-1111-111111111111@example.org:8443?security=tls#two"
	wrapped := base64.StdEncoding.EncodeToString([]byte(body))

	nodes, err := ParseSubscription([]byte(wrapped))
	if err != nil {
		t.Fatalf("ParseSubscription: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("got %d nodes, want 2", len(nodes))
	}
}

func TestParseSubscriptionAllGarbage(t *testing.T) {
	if _, err := ParseSubscription([]byte("nothing here parses as anything\nnor does this")); err == nil {
		t.Fatal("want error, got none")
	}
}

func FuzzParseSubscription(f *testing.F) {
	f.Add([]byte("trojan://secret@example.com:443#one\nvless://11111111-1111-1111-1111-111111111111@example.org:8443?security=tls#two"))
	f.Add([]byte(base64.StdEncoding.EncodeToString([]byte("ss://YWVzLTI1Ni1nY206cGFzcw==@example.com:8388#one"))))
	f.Add([]byte("proxies:\n  - {name: x, type: trojan, server: example.com, port: 443, password: secret}"))
	f.Add([]byte(""))
	f.Add([]byte("\x00\x01\xff not utf8 \xfe"))

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = ParseSubscription(data)
	})
}
