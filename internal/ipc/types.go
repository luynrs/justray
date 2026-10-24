package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/luynrs/justray/internal/domain"
)

const (
	Timeout = 30 * time.Second
	Retry   = 50 * time.Millisecond
)

type Request struct {
	Version   string
	Method    string
	Arguments Arguments
}

var ErrElevate = errors.New("elevation required")
var ErrVersion = errors.New("version mismatch")

type Arguments struct {
	NodeID           string
	SubscriptionID   string
	URL              string
	Direction        int
	Tun              *bool
	Settings         domain.Settings
	PreviousSettings domain.Settings
	Collapsed        *bool // nil toggles the current state
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

func ErrorType(err error) string {
	var joined interface{ Unwrap() []error }
	if errors.As(err, &joined) {
		kind := "failure"
		for index, cause := range joined.Unwrap() {
			causeType := ErrorType(cause)
			if index > 0 && kind != causeType {
				return "failure"
			}
			kind = causeType
		}
		return kind
	}
	switch {
	case errors.Is(err, ErrElevate):
		return "elevation"
	case errors.Is(err, ErrVersion):
		return "mismatch"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	}
	return "failure"
}

func (failure *Error) Error() string { return failure.Message }

func (failure *Error) Is(target error) bool {
	switch target {
	case ErrElevate:
		return failure.Type == "elevation"
	case ErrVersion:
		return failure.Type == "mismatch"
	case context.Canceled:
		return failure.Type == "canceled"
	case context.DeadlineExceeded:
		return failure.Type == "timeout"
	}
	return false
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
	Transport      string
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
	Busy      bool
	Error     string
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
