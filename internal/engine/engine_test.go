package engine

import (
	"context"
	"sync"
	"testing"

	"github.com/luynrs/justray/internal/domain"
)

func TestRebuilds(t *testing.T) {
	base, err := domain.Settings{}.Normalize()
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}

	rebuilds := map[string]func(*domain.Settings){
		"port":         func(s *domain.Settings) { s.Port = 1081 },
		"dns":          func(s *domain.Settings) { s.DNS = "9.9.9.9" },
		"dns hijack":   func(s *domain.Settings) { s.DNSHijack = "off" },
		"log level":    func(s *domain.Settings) { s.LogLevel = "debug" },
		"mtu":          func(s *domain.Settings) { s.TunMTU = 1400 },
		"stack":        func(s *domain.Settings) { s.TunStack = "system" },
		"strict route": func(s *domain.Settings) { s.TunStrict = "off" },
		"ip version":   func(s *domain.Settings) { s.IPVersion = "ipv4" },
		"allow lan":    func(s *domain.Settings) { s.AllowLAN = "on" },
		"direct lan":   func(s *domain.Settings) { s.BypassLocal = "off" },
		"block quic":   func(s *domain.Settings) { s.BlockQUIC = "on" },
		"direct list":  func(s *domain.Settings) { s.Direct = []string{"10.0.0.0/8"} },
		"proxy list":   func(s *domain.Settings) { s.Proxy = []string{"work.example.com"} },
		"block list":   func(s *domain.Settings) { s.Block = []string{"ads.example.com"} },
		"mode":         func(s *domain.Settings) { s.Mode = domain.DirectAll },
	}
	for name, edit := range rebuilds {
		next := base
		edit(&next)
		if !Rebuilds(base, next) {
			t.Errorf("%s: want a rebuild, got none", name)
		}
	}

	live := map[string]func(*domain.Settings){
		"probe url": func(s *domain.Settings) { s.ProbeURL = "https://example.com/204" },
		"refresh":   func(s *domain.Settings) { s.RefreshEvery = 6 },
		"autostart": func(s *domain.Settings) { s.Autostart = "on" },
		"emoji":     func(s *domain.Settings) { s.Emoji = "on" },
		"force tty": func(s *domain.Settings) { s.ForceTTY = "on" },
	}
	for name, edit := range live {
		next := base
		edit(&next)
		if Rebuilds(base, next) {
			t.Errorf("%s: want no rebuild, got one", name)
		}
	}

	if Rebuilds(base, base) {
		t.Error("identical settings asked for a rebuild")
	}

	empty := base
	empty.Direct, empty.Proxy, empty.Block = []string{}, []string{}, []string{}
	if Rebuilds(base, empty) {
		t.Error("nil and empty lists asked for a rebuild")
	}
}

func TestProbeEngine(t *testing.T) {
	s, _ := domain.Settings{}.Normalize()
	nodes := []domain.Node{
		{ID: "n1", Protocol: domain.Trojan, Server: "127.0.0.1", Port: 9991, Auth: domain.Auth{Password: "p"}},
		{ID: "n2", Protocol: domain.SS, Server: "127.0.0.1", Port: 9992, Auth: domain.Auth{Method: "aes-128-gcm", Password: "p"}},
		{ID: "n3", Protocol: domain.VLess, Server: "127.0.0.1", Port: 9993, Auth: domain.Auth{UUID: "11111111-1111-1111-1111-111111111111"}},
		{ID: "n4", Protocol: domain.VLess, Server: "127.0.0.1", Port: 9994, Auth: domain.Auth{UUID: "11111111-1111-1111-1111-111111111111"}, Reality: &domain.Reality{PublicKey: "invalid-key"}},
		{
			ID: "ss-stls", Protocol: domain.SS, Server: "127.0.0.1", Port: 9995,
			Auth: domain.Auth{Method: "aes-128-gcm", Password: "p"},
			ShadowTLS: &domain.ShadowTLS{Version: 3, Password: "p", SNI: "example.com"},
		},
		{
			ID: "wg1", Protocol: domain.WG, Server: "127.0.0.1", Port: 51820,
			WireGuard: &domain.WireGuard{
				PrivateKey: "aGVsbG93b3JsZGhlbGxvd29ybGRoZWxsb3dvcmxkMTI=",
				PeerPublicKey: "aGVsbG93b3JsZGhlbGxvd29ybGRoZWxsb3dvcmxkMTI=",
				Address: []string{"10.0.0.2/32"},
			},
		},
		{
			ID: "wg2", Protocol: domain.WG, Server: "127.0.0.1", Port: 51821,
			WireGuard: &domain.WireGuard{
				PrivateKey: "aGVsbG93b3JsZGhlbGxvd29ybGRoZWxsb3dvcmxkMTI=",
				PeerPublicKey: "aGVsbG93b3JsZGhlbGxvd29ybGRoZWxsb3dvcmxkMTI=",
				Address: []string{"10.0.0.3/32"},
			},
		},
	}
	var mu sync.Mutex
	res := make(map[string]Result)
	err := Probe(context.Background(), nodes, s, "", func(id string, r Result) {
		mu.Lock()
		res[id] = r
		mu.Unlock()
	})
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if len(res) != len(nodes) {
		t.Fatalf("expected %d results, got %d", len(nodes), len(res))
	}
}

func TestProbeCanceled(t *testing.T) {
	s, _ := domain.Settings{}.Normalize()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Probe(ctx, []domain.Node{{ID: "n1", Server: "127.0.0.1", Port: 9991}}, s, "", func(string, Result) {})
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
