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

func call[T any](c *Client, method string, args Args) (T, error) {
	var out T

	conn, err := c.dial()
	if err != nil {
		return out, err
	}
	defer func() { _ = conn.Close() }()
	timeout := IdleTimeout
	if method == "Probe" {
		timeout = 5 * time.Minute
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))

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

func (c *Client) Ping() error                     { _, err := call[any](c, "Ping", Args{}); return err }
func (c *Client) Status() (Status, error)         { return call[Status](c, "Status", Args{}) }
func (c *Client) Active() (domain.NodeRef, error) { return call[domain.NodeRef](c, "Active", Args{}) }
func (c *Client) Subs() ([]Sub, error)            { return call[[]Sub](c, "Subs", Args{}) }
func (c *Client) AddSub(url string) (Sub, error)  { return call[Sub](c, "AddSub", Args{URL: url}) }
func (c *Client) RemoveSub(id string) error {
	_, err := call[any](c, "RemoveSub", Args{ID: id})
	return err
}
func (c *Client) MoveSub(id string, dir int) error {
	_, err := call[any](c, "MoveSub", Args{ID: id, Dir: dir})
	return err
}
func (c *Client) RefreshAll() ([]Sub, error)     { return call[[]Sub](c, "RefreshAll", Args{}) }
func (c *Client) Refresh(id string) (Sub, error) { return call[Sub](c, "Refresh", Args{ID: id}) }
func (c *Client) Nodes() ([]Node, error)         { return call[[]Node](c, "Nodes", Args{}) }
func (c *Client) Connect(ref domain.NodeRef) (Status, error) {
	return call[Status](c, "Connect", Args{ID: ref.NodeID, Sub: ref.SubscriptionID})
}
func (c *Client) Disconnect() (Status, error) { return call[Status](c, "Disconnect", Args{}) }

func (c *Client) Probe(sub, id string) ([]Node, error) {
	return call[[]Node](c, "Probe", Args{Sub: sub, ID: id})
}

func (c *Client) SetTun(enable bool) (Status, error) {
	return call[Status](c, "SetTun", Args{Tun: enable})
}

func (c *Client) Settings() (domain.Settings, error) {
	return call[domain.Settings](c, "Settings", Args{})
}

func (c *Client) SetSettings(s domain.Settings) (Status, error) {
	return call[Status](c, "SetSettings", Args{Settings: s})
}

func (c *Client) Shutdown() error { _, err := call[any](c, "Shutdown", Args{}); return err }

func (c *Client) Watch(ctx context.Context, onUpdate func(Status)) error {
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
		var st Status
		if err := dec.Decode(&st); err != nil {
			return fmt.Errorf("watch: %w", err)
		}
		onUpdate(st)
	}
}
