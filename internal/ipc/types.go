package ipc

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/luynrs/justray/internal/domain"
)

type Request struct {
	Version   string
	Method    string
	Arguments Arguments
}

var ErrElevate = errors.New("elevation required")
var ErrVersion = errors.New("client and daemon version mismatch")

type Arguments struct {
	NodeID         string
	SubscriptionID string
	URL            string
	Direction      int
	Tun            *bool
	Settings       domain.Settings
	Autostart      bool
	Collapsed      *bool // nil toggles the current state
}

type Response struct {
	Version string
	Success bool
	Result  json.RawMessage
	Error   *Error
}

type Error struct {
	Type    string
	Message string
}

func (failure *Error) Error() string { return failure.Message }

func (failure *Error) Is(target error) bool {
	return target == ErrElevate && failure.Type == "elevation" || target == ErrVersion && failure.Type == "version_mismatch"
}

type Subscription struct {
	SubscriptionID string
	Name           string
	NodeCount      int
	UpdatedAt      time.Time
	Traffic        Traffic
	Refreshable    bool
	Refreshing     bool
	Warning        string
}

type Node struct {
	NodeID         string
	Name           string
	Protocol       string
	Server         string
	Port           int
	SubscriptionID string

	// false until Probe has run
	Probed   bool
	Alive    bool
	Duration int // milliseconds
	Probing  bool
	Failure  string
	Error    string
}

func (n Node) Latency() string {
	if n.Probing || !n.Probed {
		return ""
	}
	if n.Alive {
		return fmt.Sprintf("%dms", n.Duration)
	}
	return n.Failure
}

func (n Node) Ref() domain.NodeRef {
	return domain.NodeRef{SubscriptionID: n.SubscriptionID, NodeID: n.NodeID}
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
	Settings      domain.Settings
	Subscriptions []Subscription
	Nodes         []Node
	Status        Status
	Selected      domain.NodeRef
	Collapsed     []string
}

type Traffic struct {
	UploadBytes   int64
	DownloadBytes int64
	TotalBytes    int64
	ExpiresAt     time.Time
}
