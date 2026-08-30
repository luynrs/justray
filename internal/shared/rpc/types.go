package rpc

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/luynrs/justray/internal/shared/domain"
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
	ID        string
	Name      string
	Nodes     int
	UpdatedAt time.Time
	Traffic   domain.Traffic
	Direct    bool // a bare share link
}

type Node struct {
	ID       string
	Name     string
	Protocol string
	Server   string
	Port     int
	Sub      string

	// false until Probe has run
	Probed bool
	Alive  bool
	MS     int
}

func (n Node) Ref() domain.NodeRef {
	return domain.NodeRef{SubscriptionID: n.Sub, NodeID: n.ID}
}

type Status struct {
	Connected bool
	NodeRef   domain.NodeRef
	NodeName  string
	Uptime    int64 // seconds
	Port      int
	Tun       bool
}
