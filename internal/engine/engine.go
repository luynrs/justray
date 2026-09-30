package engine

import (
	"context"
	"slices"

	"github.com/luynrs/justray/internal/domain"
)

type Engine interface {
	Apply(context.Context, SessionSpec) error
	Stop() error
	Running() bool
}

type SessionSpec struct {
	Node     domain.Node
	Settings domain.Settings
	Tun      bool
}

func Rebuilds(x, y domain.Settings, tun bool) bool {
	return x.LogLevel != y.LogLevel || x.Port != y.Port || x.AllowLAN != y.AllowLAN ||
		x.IPVersion != y.IPVersion || x.DNS != y.DNS ||
		x.Mode != y.Mode || x.BypassLocal != y.BypassLocal ||
		x.BlockQUIC != y.BlockQUIC || !slices.Equal(x.Direct, y.Direct) ||
		!slices.Equal(x.Proxy, y.Proxy) || !slices.Equal(x.Block, y.Block) ||
		tun && (x.TunStack != y.TunStack || x.TunMTU != y.TunMTU || x.DNSHijack != y.DNSHijack ||
			x.TunStrict != y.TunStrict)
}

type Result struct {
	Alive    bool
	Duration int
}
