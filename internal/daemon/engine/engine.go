package engine

import (
	"context"

	"github.com/luynrs/justray/internal/shared/domain"
)

type Engine interface {
	Start(n domain.Node, tun bool) error
	Swap(n domain.Node) error
	TunAdd() error
	TunRemove() error
	Close() error
}

type Result struct {
	Alive bool
	MS    int
}

type New func(s domain.Settings, logPath string) Engine

type Probe func(context.Context, []domain.Node, domain.Settings, string) (map[string]Result, error)
