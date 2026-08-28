package connection

import (
	"testing"

	"github.com/luynrs/justray/internal/shared/domain"
)

func TestEngineChanged(t *testing.T) {
	base, err := domain.Settings{}.Normalize()
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}

	rebuilds := map[string]func(*domain.Settings){
		"port":           func(s *domain.Settings) { s.Port = 1081 },
		"dns":            func(s *domain.Settings) { s.DNS = "9.9.9.9" },
		"dns hijack":     func(s *domain.Settings) { s.DNSHijack = "off" },
		"log level":      func(s *domain.Settings) { s.LogLevel = "debug" },
		"mtu":            func(s *domain.Settings) { s.TunMTU = 1400 },
		"stack":          func(s *domain.Settings) { s.TunStack = "system" },
		"strict route":   func(s *domain.Settings) { s.TunStrict = "off" },
		"ip version":     func(s *domain.Settings) { s.IPVersion = "ipv4" },
		"local networks": func(s *domain.Settings) { s.BypassLocal = "off" },
		"block quic":     func(s *domain.Settings) { s.BlockQUIC = "on" },
		"except list":    func(s *domain.Settings) { s.Except = []string{"10.0.0.0/8"} },
		"blocked list":   func(s *domain.Settings) { s.Blocked = []string{"ads.example.com"} },
		"mode":           func(s *domain.Settings) { s.Mode = domain.DirectAll },
	}
	for name, edit := range rebuilds {
		next := base
		edit(&next)
		if !engineChanged(base, next) {
			t.Errorf("%s: want a rebuild, got none", name)
		}
	}

	live := map[string]func(*domain.Settings){
		"probe url": func(s *domain.Settings) { s.ProbeURL = "https://example.com/204" },
		"refresh":   func(s *domain.Settings) { s.RefreshEvery = 6 },
		"autostart": func(s *domain.Settings) { s.Autostart = "on" },
		"emoji":     func(s *domain.Settings) { s.Emoji = "on" },
	}
	for name, edit := range live {
		next := base
		edit(&next)
		if engineChanged(base, next) {
			t.Errorf("%s: want no rebuild, got one", name)
		}
	}

	if engineChanged(base, base) {
		t.Error("identical settings asked for a rebuild")
	}

	empty := base
	empty.Except, empty.Blocked = []string{}, []string{}
	if engineChanged(base, empty) {
		t.Error("nil and empty lists asked for a rebuild")
	}
}
