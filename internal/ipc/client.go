package ipc

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/luynrs/justray/internal/domain"
)

type Client struct {
	socket string
}

// IdleTimeout bounds how long either side waits on a quiet connection
const IdleTimeout = 60 * time.Second

func NewClient(socket string) *Client { return &Client{socket: socket} }

var ErrNoDaemon = errors.New("daemon is not running")

func (c *Client) dial(ctx context.Context) (net.Conn, error) {
	dialer := net.Dialer{Timeout: 3 * time.Second}
	conn, err := dialer.DialContext(ctx, "unix", c.socket)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if errors.Is(err, os.ErrPermission) {
			return nil, errors.New("cannot access daemon: permission denied")
		}
		return nil, ErrNoDaemon
	}
	return conn, nil
}

func call[T any](ctx context.Context, c *Client, method string, args Args) (T, error) {
	var out T
	timeout := 30 * time.Second
	switch method {
	case "Ping":
		timeout = time.Second
	case "Snapshot":
		timeout = 3 * time.Second
	case "Probe":
		timeout = 5 * time.Minute
	case "Refresh", "RefreshAll":
		timeout = 0
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	conn, err := c.dial(ctx)
	if err != nil {
		return out, err
	}
	defer func() { _ = conn.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	if err := json.NewEncoder(conn).Encode(Req{method, args}); err != nil {
		return out, cmp.Or(ctx.Err(), fmt.Errorf("%s: %w", method, err))
	}
	var resp Resp
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return out, cmp.Or(ctx.Err(), fmt.Errorf("%s: %w", method, err))
	}
	if !resp.OK {
		// Older daemons send only the message during an in-place upgrade.
		if resp.ElevationRequired != nil && *resp.ElevationRequired || resp.ElevationRequired == nil && resp.Error == ErrElevate.Error() {
			return out, ErrElevate
		}
		return out, errors.New(resp.Error)
	}
	if resp.Result != nil {
		return out, json.Unmarshal(resp.Result, &out)
	}
	return out, nil
}

func (c *Client) Ping(ctx context.Context) error {
	_, err := call[any](ctx, c, "Ping", Args{})
	return err
}
func (c *Client) Snapshot(ctx context.Context) (Snapshot, error) {
	return call[Snapshot](ctx, c, "Snapshot", Args{})
}
func (c *Client) AddSub(ctx context.Context, url string) (Sub, error) {
	return call[Sub](ctx, c, "AddSub", Args{URL: url})
}
func (c *Client) RemoveSub(ctx context.Context, id string) error {
	return c.command(ctx, "RemoveSub", Args{ID: id})
}
func (c *Client) RemoveNode(ctx context.Context, ref domain.NodeRef) error {
	return c.command(ctx, "RemoveNode", Args{ID: ref.NodeID, Sub: ref.SubscriptionID})
}
func (c *Client) MoveSub(ctx context.Context, id string, dir int) error {
	return c.command(ctx, "MoveSub", Args{ID: id, Dir: dir})
}
func (c *Client) RefreshAll(ctx context.Context) error { return c.command(ctx, "RefreshAll", Args{}) }
func (c *Client) Refresh(ctx context.Context, id string) error {
	return c.command(ctx, "Refresh", Args{ID: id})
}
func (c *Client) Connect(ctx context.Context, ref domain.NodeRef, mode *bool) error {
	return c.command(ctx, "Connect", Args{ID: ref.NodeID, Sub: ref.SubscriptionID, Mode: mode})
}
func (c *Client) Disconnect(ctx context.Context) error { return c.command(ctx, "Disconnect", Args{}) }

func (c *Client) Probe(ctx context.Context, sub, id string) error {
	return c.command(ctx, "Probe", Args{Sub: sub, ID: id})
}

func (c *Client) SetTun(ctx context.Context, enable bool) error {
	return c.command(ctx, "SetTun", Args{Tun: enable})
}

func (c *Client) SetSettings(ctx context.Context, s domain.Settings) error {
	return c.command(ctx, "SetSettings", Args{Settings: s})
}

func (c *Client) SetAutostart(ctx context.Context, enabled bool) error {
	return c.command(ctx, "SetAutostart", Args{Autostart: enabled})
}

func (c *Client) SetCollapsed(ctx context.Context, id string, collapsed bool) error {
	return c.command(ctx, "SetCollapsed", Args{ID: id, Collapsed: collapsed})
}

func (c *Client) Shutdown(ctx context.Context) error { return c.command(ctx, "Shutdown", Args{}) }

func (c *Client) command(ctx context.Context, method string, args Args) error {
	_, err := call[struct{}](ctx, c, method, args)
	return err
}

func (c *Client) Watch(ctx context.Context, onUpdate func(Snapshot)) error {
	conn, err := c.dial(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	if err := json.NewEncoder(conn).Encode(Req{Method: "Watch"}); err != nil {
		return fmt.Errorf("watch: %w", err)
	}
	dec := json.NewDecoder(conn)
	for {
		var snap Snapshot
		if err := dec.Decode(&snap); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("watch: %w", err)
		}
		onUpdate(snap)
	}
}
