package engine

import (
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
