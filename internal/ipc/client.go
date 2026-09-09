package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/luynrs/justray/internal/domain"
)

type Client struct{ socket string }

// IdleTimeout bounds how long either side waits on a quiet connection
const IdleTimeout = 60 * time.Second

func NewClient(socket string) *Client { return &Client{socket} }

func (c *Client) dial(ctx context.Context) (net.Conn, error) {
	dialer := net.Dialer{Timeout: 3 * time.Second}
	conn, err := dialer.DialContext(ctx, "unix", c.socket)
	if err != nil {
		return nil, fmt.Errorf("no daemon on %s", c.socket)
	}
	return conn, nil
}

func timeoutFor(method string) time.Duration {
	switch method {
	case "Ping":
		return time.Second
	case "Snapshot":
		return 3 * time.Second
	case "Probe":
		return 5 * time.Minute
	default:
		return 30 * time.Second
	}
}

func call[T any](c *Client, method string, args Args) (T, error) {
	var out T

	conn, err := c.dial(context.Background())
	if err != nil {
		return out, err
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(timeoutFor(method)))

	if err := json.NewEncoder(conn).Encode(Req{method, args}); err != nil {
		return out, fmt.Errorf("%s: %w", method, err)
	}
	var resp Resp
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return out, fmt.Errorf("%s: %w", method, err)
	}
	if !resp.OK {
		return out, errors.New(resp.Error)
	}
	if resp.Result != nil {
		return out, json.Unmarshal(resp.Result, &out)
	}
	return out, nil
}

func (c *Client) Ping() error                 { _, err := call[any](c, "Ping", Args{}); return err }
func (c *Client) Snapshot() (Snapshot, error) { return call[Snapshot](c, "Snapshot", Args{}) }
func (c *Client) AddSub(url string) (Sub, error) {
	return call[Sub](c, "AddSub", Args{URL: url})
}
func (c *Client) RemoveSub(id string) error {
	return c.command("RemoveSub", Args{ID: id})
}
func (c *Client) MoveSub(id string, dir int) error {
	return c.command("MoveSub", Args{ID: id, Dir: dir})
}
func (c *Client) RefreshAll() error { return c.command("RefreshAll", Args{}) }
func (c *Client) Refresh(id string) error {
	return c.command("Refresh", Args{ID: id})
}
func (c *Client) Connect(ref domain.NodeRef) error {
	return c.command("Connect", Args{ID: ref.NodeID, Sub: ref.SubscriptionID})
}
func (c *Client) Disconnect() error { return c.command("Disconnect", Args{}) }

func (c *Client) Probe(sub, id string) error {
	return c.command("Probe", Args{Sub: sub, ID: id})
}

func (c *Client) SetTun(enable bool) error {
	return c.command("SetTun", Args{Tun: enable})
}

func (c *Client) SetSettings(s domain.Settings) error {
	return c.command("SetSettings", Args{Settings: s})
}

func (c *Client) Shutdown() error { return c.command("Shutdown", Args{}) }

func (c *Client) command(method string, args Args) error {
	_, err := call[struct{}](c, method, args)
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
			return fmt.Errorf("watch: %w", err)
		}
		onUpdate(snap)
	}
}
