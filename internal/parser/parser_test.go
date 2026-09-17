package parser

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/luynrs/justray/internal/domain"
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
		"tuic5":       {"tuic5://11111111-1111-1111-1111-111111111111:pass@example.com:443#node", domain.TUIC, "example.com", 443},
		"tuicv5":      {"tuicv5://11111111-1111-1111-1111-111111111111:pass@example.com:443#node", domain.TUIC, "example.com", 443},
		"anytls":      {"anytls://secret@example.com:443#node", domain.AnyTLS, "example.com", 443},
		"socks5":      {"socks5://user:pass@example.com:1080#node", domain.SOCKS, "example.com", 1080},
		"socks alias": {"socks://user:pass@example.com:1080#node", domain.SOCKS, "example.com", 1080},
		"wireguard":   {"wireguard://priv%2Fkey@example.com:51820?publickey=pub%2Bkey&address=10.0.0.2%2F32#node", domain.WG, "example.com", 51820},
		"wg alias":    {"wg://priv@example.com:51820?publickey=pub&address=10.0.0.2%2F32#node", domain.WG, "example.com", 51820},
		"shadowtls":   {"shadowtls://:secret@example.com:443?version=3&sni=cloud.example#node", domain.Shadow, "example.com", 443},
		"stls alias":  {"shadow-tls://secret@example.com:443#node", domain.Shadow, "example.com", 443},
		"stls":        {"stls://secret@example.com:443#node", domain.Shadow, "example.com", 443},
		"http":        {"http://user:pass@example.com:80#node", domain.HTTP, "example.com", 80},
		"https":       {"https://user:pass@example.com:443#node", domain.HTTP, "example.com", 443},
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
	for _, uri := range []string{
		"ss://YWVzLTI1Ni1nY206cGFzcw==@example.com:8388?plugin=shadow-tls%3Bhost%3Dcloud.example%3Bpassword%3Dsecret%3Bversion%3D2#node",
		"ss://" + base64.StdEncoding.EncodeToString([]byte("aes-128-gcm:pass@example.com:8388?plugin=shadow-tls;sni=cloud.example;password=secret;version=2")),
	} {
		n, err := ParseURI(uri)
		if err != nil || n.ShadowTLS == nil || n.ShadowTLS.Version != 2 || n.ShadowTLS.SNI != "cloud.example" {
			t.Fatalf("got %#v, err %v", n.ShadowTLS, err)
		}
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
		"https://youtu.be/dQw4w9WgXcQ", // rickroll
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

func TestParseSubscriptionURIWithClashMarker(t *testing.T) {
	nodes, err := ParseSubscription([]byte("trojan://secret@example.com:443#proxies:"))
	if err != nil || len(nodes) != 1 {
		t.Fatalf("got %d nodes, err %v", len(nodes), err)
	}
}

func TestParseSubscriptionXray(t *testing.T) {
	jsonPayload := `[{"remarks":"Germany VLESS","outbounds":[{"tag":"proxy","protocol":"vless","settings":{"vnext":[{"address":"1.2.3.4","port":443,"users":[{"id":"11111111-1111-1111-1111-111111111111","flow":"xtls-rprx-vision"}]}]},"streamSettings":{"network":"tcp","security":"reality","realitySettings":{"serverName":"example.com","publicKey":"pubkey","shortId":"shortid"}}},{"protocol":"vmess","settings":{"vnext":[{"address":"1.2.3.4","port":443,"users":[{"id":"22222222-2222-2222-2222-222222222222"}]}]}},{"tag":"direct","protocol":"freedom"}]}]`

	nodes, err := ParseSubscription([]byte(jsonPayload))
	if err != nil || len(nodes) != 2 || nodes[1].Protocol != domain.VMess {
		t.Fatalf("ParseSubscription Xray: err=%v, nodes=%+v", err, nodes)
	}
	n := nodes[0]
	if n.Name != "Germany VLESS" || n.Protocol != domain.VLess || n.Server != "1.2.3.4" || n.Port != 443 {
		t.Fatalf("unexpected node: %+v", n)
	}
	if n.Reality == nil || n.Reality.PublicKey != "pubkey" || n.Reality.ShortID != "shortid" {
		t.Fatalf("unexpected reality: %+v", n.Reality)
	}
}

func TestParseSubscriptionXrayMultiOutbound(t *testing.T) {
	jsonPayload := `[{"remarks":"Switzerland","outbounds":[{"tag":"proxy-decoy-1-direct","protocol":"vless","settings":{"vnext":[{"address":"87.84.224.105","port":443,"users":[{"id":"uuid1","flow":"xtls-rprx-vision"}]}]},"streamSettings":{"network":"tcp","security":"reality","realitySettings":{"serverName":"vk.com","publicKey":"pk1","shortId":"s1"}}},{"tag":"proxy-wl-1-direct","protocol":"vless","settings":{"vnext":[{"address":"46.243.142.42","port":9443,"users":[{"id":"uuid1"}]}]},"streamSettings":{"network":"grpc","security":"reality","realitySettings":{"serverName":"yandex.net","publicKey":"pk2","shortId":"s2"},"grpcSettings":{"serviceName":"proxy"}}}]}]`

	nodes, err := ParseSubscription([]byte(jsonPayload))
	if err != nil {
		t.Fatalf("ParseSubscription Xray: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("got %d nodes, want 2", len(nodes))
	}
	if nodes[0].Name != "Switzerland (proxy-decoy-1-direct)" {
		t.Errorf("unexpected name %q", nodes[0].Name)
	}
	if nodes[1].Name != "Switzerland (proxy-wl-1-direct)" {
		t.Errorf("unexpected name %q", nodes[1].Name)
	}
}

func TestParseSubscriptionXrayMultipleEndpoints(t *testing.T) {
	body := `{"outbounds":[{"protocol":"vless","settings":{"vnext":[{"address":"one.example","port":443,"users":[{"id":"one"},{"id":"two"}]},{"address":"two.example","port":8443,"users":[{"id":"three"}]}]}}]}`
	nodes, err := ParseSubscription([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 3 || nodes[2].Server != "two.example" || nodes[2].Auth.UUID != "three" {
		t.Fatalf("unexpected nodes: %+v", nodes)
	}
}

func TestParseSubscriptionRejectsInvalidXray(t *testing.T) {
	for _, body := range []string{
		`{"outbounds":[{"protocol":"vless","settings":{"vnext":[{"port":443,"users":[{"id":"uuid"}]}]}}]}`,
		`{"outbounds":[{"protocol":"vless","settings":{"vnext":[{"address":"example.com","port":443,"users":[{"id":"uuid"}]}]},"streamSettings":{"network":"invalid"}}]}`,
	} {
		if _, err := ParseSubscription([]byte(body)); err == nil {
			t.Fatalf("accepted invalid xray config: %s", body)
		}
	}
}

func TestParseXHTTPExtra(t *testing.T) {
	raw := `{"mode":"packet-up","path":"/uploadfiles/","xPaddingKey":"_dc","xPaddingHeader":"X-Cache","xPaddingMethod":"tokenish","uplinkHTTPMethod":"GET","xPaddingObfsMode":true,"xPaddingPlacement":"queryInHeader"}`
	n, err := ParseURI(xhttpURI(raw))
	if err != nil {
		t.Fatalf("ParseURI: %v", err)
	}
	if n.Transport.Network != "xhttp" {
		t.Fatalf("unexpected network: %s", n.Transport.Network)
	}
	if n.Transport.Extra != raw {
		t.Fatalf("unexpected Extra: got %q, want %q", n.Transport.Extra, raw)
	}
}

func TestParseVMessXHTTPExtra(t *testing.T) {
	raw := `{"mode":"packet-up","uplinkHTTPMethod":"GET"}`
	vm := `{"add":"example.com","port":"443","id":"11111111-1111-1111-1111-111111111111","net":"xhttp","extra":"` + strings.ReplaceAll(raw, `"`, `\"`) + `"}`
	n, err := ParseURI("vmess://" + base64.StdEncoding.EncodeToString([]byte(vm)))
	if err != nil {
		t.Fatalf("ParseURI vmess: %v", err)
	}
	if n.Transport.Network != "xhttp" || n.Transport.Extra != raw {
		t.Fatalf("unexpected transport: %+v", n.Transport)
	}
}

func xhttpURI(extra string) string {
	return "vless://11111111-1111-1111-1111-111111111111@example.com:443?type=xhttp&extra=" + url.QueryEscape(extra)
}

func TestParseHysteria2Obfs(t *testing.T) {
	n, err := ParseURI("hy2://example.com:443?password=secret&obfs-param=xyz&pinSHA256=" + strings.Repeat("a", 64))
	if err != nil || n.Auth.Password != "secret" || n.Obfs != "salamander" || n.ObfsPassword != "xyz" || !n.TLS.Insecure {
		t.Fatalf("unexpected hy2: err=%v, node=%+v", err, n)
	}
}

func TestParseWireGuardReserved(t *testing.T) {
	n, err := ParseURI("wg://private@example.com:51820?publickey=public&address=10.0.0.2/32&reserved=1,2,3")
	if err != nil || len(n.WireGuard.Reserved) != 3 || n.WireGuard.Reserved[0] != 1 || n.WireGuard.Reserved[1] != 2 || n.WireGuard.Reserved[2] != 3 {
		t.Fatalf("unexpected wg: err=%v, reserved=%v", err, n.WireGuard.Reserved)
	}
	n, err = ParseURI("wg://private@example.com:51820?publickey=public&address=10.0.0.2/32&reserved=+wAA")
	if err != nil || !slices.Equal(n.WireGuard.Reserved, []byte{251, 0, 0}) {
		t.Fatalf("unexpected base64 reserved: err=%v, reserved=%v", err, n.WireGuard.Reserved)
	}
	n, err = ParseURI("wireguard://example.com:51820?private_key=priv&public_key=pub&preshared_key=psk&address=10.0.0.2/32")
	if err != nil || n.WireGuard.PeerPublicKey != "pub" || n.WireGuard.PreSharedKey != "psk" {
		t.Fatalf("unexpected wg keys: err=%v, wg=%+v", err, n.WireGuard)
	}
}

func TestParseVLessImplicitReality(t *testing.T) {
	n, err := ParseURI("vless://11111111-1111-1111-1111-111111111111@example.com:443?publicKey=publickey&shortId=1234&sni=example.com&packet-encoding=packetaddr&skip-cert-verify=1&service_name=svc&type=grpc")
	if err != nil || n.Reality == nil || n.Reality.PublicKey != "publickey" || n.Reality.ShortID != "1234" || n.PacketEncoding != "packetaddr" || !n.TLS.Insecure || n.Transport.ServiceName != "svc" {
		t.Fatalf("unexpected implicit reality: err=%v, node=%+v", err, n)
	}
}

func TestParseTUICCongestion(t *testing.T) {
	n, err := ParseURI("tuic://11111111-1111-1111-1111-111111111111:pass@example.com:443?congestion-control=cubic&udp-relay-mode=quic")
	if err != nil || n.Congestion != "cubic" || n.UDPRelayMode != "quic" || n.TLS == nil || n.TLS.ALPN[0] != "h3" {
		t.Fatalf("unexpected tuic: err=%v, congestion=%q, udp=%q, tls=%+v", err, n.Congestion, n.UDPRelayMode, n.TLS)
	}
}

func TestParseHysteriaAuth(t *testing.T) {
	for _, uri := range []string{
		"hysteria://example.com:443?auth=secret_token&upmbps=50&downmbps=200",
		"hysteria://example.com:443?auth_str=secret_token&upmbps=50&downmbps=200",
		"hysteria://secret_token@example.com:443?upmbps=50&downmbps=200",
		"hysteria://:secret_token@example.com:443",
	} {
		n, err := ParseURI(uri)
		if err != nil {
			t.Fatalf("ParseURI(%q): %v", uri, err)
		}
		if n.Auth.Password != "secret_token" {
			t.Errorf("ParseURI(%q): got auth %q, want %q", uri, n.Auth.Password, "secret_token")
		}
	}
}

func TestParseSOCKSAuth(t *testing.T) {
	n, err := ParseURI("socks5://user:pass@example.com:1080")
	if err != nil || n.Auth.Username != "user" || n.Auth.Password != "pass" {
		t.Fatalf("plain user:pass failed: %+v, err=%v", n.Auth, err)
	}
	n, err = ParseURI("socks5://" + base64.StdEncoding.EncodeToString([]byte("user:pass")) + "@example.com:1080")
	if err != nil || n.Auth.Username != "user" || n.Auth.Password != "pass" {
		t.Fatalf("base64 user:pass failed: %+v, err=%v", n.Auth, err)
	}
}

func TestParseSubscriptionAllGarbage(t *testing.T) {
	if _, err := ParseSubscription([]byte("nothing here parses as anything\nnor does this")); err == nil {
		t.Fatal("want error, got none")
	}
}

func TestParseClashFingerprints(t *testing.T) {
	yaml := "proxies:\n" +
		"  - {name: cert, type: trojan, server: example.com, port: 443, password: p, fingerprint: " + strings.Repeat("a", 64) + "}\n" +
		"  - {name: utls, type: trojan, server: example.com, port: 443, password: p, client-fingerprint: firefox, fingerprint: " + strings.Repeat("a", 64) + "}\n" +
		"  - {name: stls, type: shadow-tls, server: example.com, port: 443, password: p, server_name: cloud.example}\n" +
		"  - {name: wg, type: wg, server: example.com, port: 51820, private-key: priv, public-key: pub, address: 10.0.0.2/32}\n"
	nodes, err := ParseSubscription([]byte(yaml))
	if err != nil || len(nodes) != 4 || !nodes[0].TLS.Insecure || nodes[1].TLS.Fingerprint != "firefox" || nodes[2].Protocol != domain.Shadow || nodes[3].Protocol != domain.WG {
		t.Fatalf("unexpected nodes: %v, err=%v", nodes, err)
	}
}

func TestParseClashProtocols(t *testing.T) {
	yaml := `proxies:
  - name: vl
    type: vless
    server: 1.1.1.1
    port: 443
    uuid: 11111111-1111-1111-1111-111111111111
    reality-opts:
      public-key: pub
      short-id: "1234"
  - name: vm
    type: vmess
    server: 1.1.1.1
    port: 443
    uuid: 11111111-1111-1111-1111-111111111111
    alterId: 0
    cipher: auto
    tls: true
  - name: ss
    type: ss
    server: 1.1.1.1
    port: 8388
    cipher: aes-128-gcm
    password: pass
    plugin: shadow-tls
    plugin-opts:
      host: cloud.example
      password: sec
      version: 3
  - name: hy2
    type: hysteria2
    server: 1.1.1.1
    port: 443
    password: pass
    obfs: salamander
    obfs-password: obfs
  - name: hy1
    type: hysteria
    server: 1.1.1.1
    port: 443
    auth-str: pass
    up: "100 Mbps"
    down: "200 Mbps"
  - name: tc
    type: tuic
    server: 1.1.1.1
    port: 443
    uuid: 11111111-1111-1111-1111-111111111111
    password: pass
  - name: at
    type: anytls
    server: 1.1.1.1
    port: 443
    password: pass
  - name: hp
    type: http
    server: 1.1.1.1
    port: 8080
    username: u
    password: p
`
	nodes, err := ParseSubscription([]byte(yaml))
	if err != nil {
		t.Fatalf("ParseSubscription Clash: %v", err)
	}
	if len(nodes) != 8 {
		t.Fatalf("got %d nodes, want 8", len(nodes))
	}
	if nodes[0].Protocol != domain.VLess || nodes[0].Reality == nil || nodes[0].Reality.PublicKey != "pub" {
		t.Fatalf("unexpected vless: %+v", nodes[0])
	}
	if nodes[1].Protocol != domain.VMess || nodes[1].TLS == nil {
		t.Fatalf("unexpected vmess: %+v", nodes[1])
	}
	if nodes[2].Protocol != domain.SS || nodes[2].ShadowTLS == nil || nodes[2].ShadowTLS.SNI != "cloud.example" {
		t.Fatalf("unexpected ss: %+v", nodes[2])
	}
	if nodes[3].Protocol != domain.HY2 || nodes[3].Obfs != "salamander" {
		t.Fatalf("unexpected hy2: %+v", nodes[3])
	}
	if nodes[4].Protocol != domain.HY1 || nodes[4].UpMbps != 100 || nodes[4].DownMbps != 200 {
		t.Fatalf("unexpected hy1: %+v", nodes[4])
	}
	if nodes[5].Protocol != domain.TUIC || nodes[5].Auth.UUID == "" {
		t.Fatalf("unexpected tuic: %+v", nodes[5])
	}
	if nodes[6].Protocol != domain.AnyTLS {
		t.Fatalf("unexpected anytls: %+v", nodes[6])
	}
	if nodes[7].Protocol != domain.HTTP || nodes[7].Auth.Username != "u" {
		t.Fatalf("unexpected http: %+v", nodes[7])
	}
}

func TestParseVMessOptions(t *testing.T) {
	for _, raw := range []string{
		`{"add":"example.com","port":"443","id":"11111111-1111-1111-1111-111111111111","tls":"1","allowInsecure":"true","fp":"` + strings.Repeat("a", 64) + `"}`,
		`{"add":"example.com","port":443,"id":"11111111-1111-1111-1111-111111111111","tls":true,"allowInsecure":true}`,
	} {
		n, err := ParseURI("vmess://" + base64.StdEncoding.EncodeToString([]byte(raw)))
		if err != nil || n.TLS == nil || !n.TLS.Insecure {
			t.Fatalf("unexpected vmess: err=%v, tls=%+v", err, n.TLS)
		}
	}
}

func TestIsLinkHTTP(t *testing.T) {
	for _, l := range []string{
		"http://user:pass@example.com:8080#node",
		"https://user:pass@example.com:8443#node",
		"http://example.com:8080#node",
		"https://example.com:8443#node",
	} {
		if !IsLink(l) {
			t.Errorf("IsLink(%q) = false, want true", l)
		}
	}
	for _, s := range []string{
		"https://example.com/sub/token",
		"https://example.com:8443/sub?token=abc#MySub",
		"https://example.com",
	} {
		if IsLink(s) {
			t.Errorf("IsLink(%q) = true, want false", s)
		}
	}
}

func TestParseHTTPProxy(t *testing.T) {
	n, err := ParseURI("http://user:pass@example.com:8080#proxy1")
	if err != nil || n.Protocol != domain.HTTP || n.Auth.Username != "user" || n.Auth.Password != "pass" || n.Port != 8080 || n.TLS != nil {
		t.Fatalf("unexpected http node: err=%v, node=%+v", err, n)
	}
	n2, err := ParseURI("https://example.com:8443#proxy2")
	if err != nil || n2.Protocol != domain.HTTP || n2.Port != 8443 || n2.TLS == nil || n2.TLS.SNI != "example.com" {
		t.Fatalf("unexpected https node: err=%v, node=%+v", err, n2)
	}
	n3, err := ParseURI("http://user:pass@example.com#proxy3")
	if err != nil || n3.Protocol != domain.HTTP || n3.Port != 80 {
		t.Fatalf("unexpected default http port: err=%v, node=%+v", err, n3)
	}
	n4, err := ParseURI("https://user:pass@example.com#proxy4")
	if err != nil || n4.Protocol != domain.HTTP || n4.Port != 443 || n4.TLS == nil {
		t.Fatalf("unexpected default https port: err=%v, node=%+v", err, n4)
	}
}

func TestParseXrayTrojanSS(t *testing.T) {
	raw := `{"remarks":"test","outbounds":[
		{"tag":"tr","protocol":"trojan","settings":{"servers":[{"address":"example.com","port":443,"password":"pass"}]},"streamSettings":{"security":"tls"}},
		{"tag":"ss","protocol":"shadowsocks","settings":{"servers":[{"address":"example.com","port":8388,"method":"aes-128-gcm","password":"pass"}]}}
	]}`
	nodes, err := ParseSubscription([]byte(raw))
	if err != nil || len(nodes) != 2 || nodes[0].Protocol != domain.Trojan || nodes[1].Protocol != domain.SS {
		t.Fatalf("unexpected nodes: %v, err=%v", nodes, err)
	}
}

func TestParseSingBox(t *testing.T) {
	raw := `{"outbounds":[
		{"type":"selector","tag":"select","outbounds":["vless-out"]},
		{"type":"vless","tag":"vl","server":"example.com","server_port":443,"uuid":"11111111-1111-1111-1111-111111111111","tls":{"enabled":true,"server_name":"example.com","reality":{"enabled":true,"public_key":"pub","short_id":"1234"}},"transport":{"type":"ws","path":"/ws"}},
		{"type":"shadowtls","tag":"stls","server":"example.com","server_port":443,"password":"p","version":3},
		{"type":"shadowsocks","tag":"ss","server":"example.com","server_port":8388,"method":"aes-128-gcm","password":"p","detour":"stls"},
		{"type":"hysteria2","tag":"hy2","server":"example.com","server_port":443,"password":"p","obfs":{"type":"salamander","password":"obfs"}},
		{"type":"wireguard","tag":"wg","server":"example.com","server_port":51820,"private_key":"priv","local_address":["10.0.0.2/32"],"peers":[{"public_key":"pub","reserved":[1,2,3]}]}
	]}`
	nodes, err := ParseSubscription([]byte(raw))
	if err != nil || len(nodes) != 4 {
		t.Fatalf("unexpected nodes len %d, err=%v", len(nodes), err)
	}
	if nodes[0].Protocol != domain.VLess || nodes[0].Reality == nil || nodes[0].Transport.Network != "ws" {
		t.Fatalf("unexpected vless: %+v", nodes[0])
	}
	if nodes[1].Protocol != domain.SS || nodes[1].ShadowTLS == nil || nodes[1].ShadowTLS.Version != 3 {
		t.Fatalf("unexpected ss+stls: %+v", nodes[1])
	}
	if nodes[2].Protocol != domain.HY2 || nodes[2].Obfs != "salamander" || nodes[2].ObfsPassword != "obfs" {
		t.Fatalf("unexpected hy2: %+v", nodes[2])
	}
	if nodes[3].Protocol != domain.WG || nodes[3].WireGuard.PeerPublicKey != "pub" || len(nodes[3].WireGuard.Reserved) != 3 || nodes[3].WireGuard.Reserved[0] != 1 {
		t.Fatalf("unexpected wg: %+v", nodes[3])
	}
}

func FuzzParseSubscription(f *testing.F) {
	f.Add([]byte("trojan://secret@example.com:443#one\nvless://11111111-1111-1111-1111-111111111111@example.org:8443?security=tls#two"))
	f.Add([]byte(base64.StdEncoding.EncodeToString([]byte("ss://YWVzLTI1Ni1nY206cGFzcw==@example.com:8388#one"))))
	f.Add([]byte("proxies:\n  - {name: x, type: trojan, server: example.com, port: 443, password: secret}"))
	f.Add([]byte(`[{"remarks":"test","outbounds":[{"tag":"proxy","protocol":"vless","settings":{"vnext":[{"address":"1.1.1.1","port":443,"users":[{"id":"uuid"}]}]}}]}]`))
	f.Add([]byte(`{"outbounds":[{"type":"vless","server":"1.1.1.1","server_port":443,"uuid":"11111111-1111-1111-1111-111111111111"}]}`))
	f.Add([]byte(""))
	f.Add([]byte("\x00\x01\xff not utf8 \xfe"))

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = ParseSubscription(data)
	})
}
