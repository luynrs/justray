package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/luynrs/justray/internal/shared/domain"
)

type Client struct{ socket string }

// IdleTimeout bounds how long either side waits on a quiet connection
const IdleTimeout = 60 * time.Second

func NewClient(socket string) *Client { return &Client{socket} }

func (c *Client) dial() (net.Conn, error) {
	conn, err := net.DialTimeout("unix", c.socket, 3*time.Second)
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

	conn, err := c.dial()
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
func (c *Client) AddSub(url string) (Snapshot, error) {
	return call[Snapshot](c, "AddSub", Args{URL: url})
}
func (c *Client) RemoveSub(id string) (Snapshot, error) {
	return call[Snapshot](c, "RemoveSub", Args{ID: id})
}
func (c *Client) MoveSub(id string, dir int) (Snapshot, error) {
	return call[Snapshot](c, "MoveSub", Args{ID: id, Dir: dir})
}
func (c *Client) RefreshAll() (Snapshot, error) { return call[Snapshot](c, "RefreshAll", Args{}) }
func (c *Client) Refresh(id string) (Snapshot, error) {
	return call[Snapshot](c, "Refresh", Args{ID: id})
}
func (c *Client) Connect(ref domain.NodeRef) (Snapshot, error) {
	return call[Snapshot](c, "Connect", Args{ID: ref.NodeID, Sub: ref.SubscriptionID})
}
func (c *Client) Disconnect() (Snapshot, error) { return call[Snapshot](c, "Disconnect", Args{}) }

func (c *Client) Probe(sub, id string) (Snapshot, error) {
	return call[Snapshot](c, "Probe", Args{Sub: sub, ID: id})
}

func (c *Client) SetTun(enable bool) (Snapshot, error) {
	return call[Snapshot](c, "SetTun", Args{Tun: enable})
}

func (c *Client) SetSettings(s domain.Settings) (Snapshot, error) {
	return call[Snapshot](c, "SetSettings", Args{Settings: s})
}

func (c *Client) Shutdown() error { _, err := call[any](c, "Shutdown", Args{}); return err }

func (c *Client) Watch(ctx context.Context, onUpdate func(Changed)) error {
	conn, err := c.dial()
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
		var changed Changed
		if err := dec.Decode(&changed); err != nil {
			return fmt.Errorf("watch: %w", err)
		}
		onUpdate(changed)
	}
}
