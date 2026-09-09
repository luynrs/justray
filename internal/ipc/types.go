package ipc

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/luynrs/justray/internal/domain"
)

type Req struct {
	Method string
	Args   Args
}

var ErrElevate = errors.New("granting permissions")

type Args struct {
	ID       string
	Sub      string
	URL      string
	Dir      int
	Tun      bool
	Settings domain.Settings
}

type Resp struct {
	OK     bool
	Result json.RawMessage
	Error  string
}

type Sub struct {
	ID         string
	Name       string
	Nodes      int
	UpdatedAt  time.Time
	Traffic    domain.Traffic
	Direct     bool // a bare share link
	Refreshing bool
}

type Node struct {
	ID       string
	Name     string
	Protocol string
	Server   string
	Port     int
	Sub      string

	// false until Probe has run
	Probed  bool
	Alive   bool
	MS      int
	Probing bool
}

func (n Node) Ref() domain.NodeRef {
	return domain.NodeRef{SubscriptionID: n.Sub, NodeID: n.ID}
}

type Status struct {
	Connected bool
	NodeRef   domain.NodeRef
	NodeName  string
	StartedAt time.Time
	Port      int
	Tun       bool
}

func (s Status) Uptime() time.Duration {
	if !s.Connected || s.StartedAt.IsZero() {
		return 0
	}
	return max(time.Since(s.StartedAt), 0)
}

type Snapshot struct {
	Revision      uint64
	Settings      domain.Settings
	Subscriptions []Sub
	Nodes         []Node
	Status        Status
	Active        domain.NodeRef
}
