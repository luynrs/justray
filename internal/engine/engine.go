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

func Rebuilds(x, y domain.Settings) bool {
	return x.LogLevel != y.LogLevel || x.Connection != y.Connection ||
		x.Mode != y.Mode || x.BypassLocal != y.BypassLocal || x.TunStrict != y.TunStrict ||
		x.BlockQUIC != y.BlockQUIC || !slices.Equal(x.Direct, y.Direct) ||
		!slices.Equal(x.Proxy, y.Proxy) || !slices.Equal(x.Block, y.Block)
}

type Result struct {
	Alive bool
	MS    int
}
