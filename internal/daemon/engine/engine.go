package engine

import (
	"context"

	"github.com/luynrs/justray/internal/shared/domain"
)

type Engine interface {
	Apply(context.Context, SessionSpec) error
	Stop(context.Context) error
	Running() bool
}

type SessionSpec struct {
	Node     domain.Node
	Settings domain.Settings
	Tun      bool
}

func Rebuilds(x, y domain.Settings) bool {
	x.ProbeURL, y.ProbeURL = "", ""
	x.RefreshEvery, y.RefreshEvery = 0, 0
	x.Autostart, y.Autostart = "", ""
	x.Emoji, y.Emoji = "", ""
	return !x.Equal(y)
}

type Result struct {
	Alive bool
	MS    int
}

type New func(context.Context, string) Engine

type Probe func(context.Context, []domain.Node, domain.Settings, string) (map[string]Result, error)
