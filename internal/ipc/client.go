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
	"github.com/luynrs/justray/internal/version"
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

func receive[T any](decoder *json.Decoder) (T, error) {
	var out T
	var response struct {
		Version string
		Success bool
		Result  json.RawMessage
		Error   json.RawMessage
	}
	if err := decoder.Decode(&response); err != nil {
		return out, err
	}
	if response.Version != version.Version {
		daemonVersion := cmp.Or(response.Version, "unversioned")
		return out, fmt.Errorf("%w (client %s, daemon %s); restart the daemon", ErrVersion, version.Version, daemonVersion)
	}
	if !response.Success {
		if len(response.Error) == 0 {
			return out, errors.New("daemon returned an empty error")
		}
		var failure *Error
		if err := json.Unmarshal(response.Error, &failure); err != nil {
			return out, err
		}
		if failure == nil {
			return out, errors.New("daemon returned an empty error")
		}
		return out, failure
	}
	if response.Result != nil {
		return out, json.Unmarshal(response.Result, &out)
	}
	return out, nil
}

func call[T any](ctx context.Context, c *Client, method string, args Arguments) (T, error) {
	var out T
	timeout := 30 * time.Second
	switch method {
	case "Ping":
		timeout = time.Second
	case "Snapshot":
		timeout = 3 * time.Second
	case "Probe":
		timeout = 5 * time.Minute
	case "RefreshSubscription", "RefreshSubscriptions":
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

	if err := json.NewEncoder(conn).Encode(Request{Version: version.Version, Method: method, Arguments: args}); err != nil {
		return out, cmp.Or(ctx.Err(), fmt.Errorf("%s: %w", method, err))
	}
	out, err = receive[T](json.NewDecoder(conn))
	return out, cmp.Or(ctx.Err(), err)
}

func (c *Client) Ping(ctx context.Context) error {
	_, err := call[any](ctx, c, "Ping", Arguments{})
	return err
}
func (c *Client) Snapshot(ctx context.Context) (Snapshot, error) {
	return call[Snapshot](ctx, c, "Snapshot", Arguments{})
}
func (c *Client) AddSubscription(ctx context.Context, url string) (Subscription, error) {
	return call[Subscription](ctx, c, "AddSubscription", Arguments{URL: url})
}
func (c *Client) RemoveSubscription(ctx context.Context, id string) error {
	return c.command(ctx, "RemoveSubscription", Arguments{SubscriptionID: id})
}
func (c *Client) RemoveNode(ctx context.Context, ref domain.NodeRef) error {
	return c.command(ctx, "RemoveNode", Arguments{NodeID: ref.NodeID, SubscriptionID: ref.SubscriptionID})
}
func (c *Client) MoveSubscription(ctx context.Context, id string, dir int) error {
	return c.command(ctx, "MoveSubscription", Arguments{SubscriptionID: id, Direction: dir})
}
func (c *Client) RefreshSubscriptions(ctx context.Context) error {
	return c.command(ctx, "RefreshSubscriptions", Arguments{})
}
func (c *Client) RefreshSubscription(ctx context.Context, id string) error {
	return c.command(ctx, "RefreshSubscription", Arguments{SubscriptionID: id})
}
func (c *Client) Connect(ctx context.Context, ref domain.NodeRef, mode *bool) error {
	return c.command(ctx, "Connect", Arguments{NodeID: ref.NodeID, SubscriptionID: ref.SubscriptionID, Tun: mode})
}
func (c *Client) Disconnect(ctx context.Context) error {
	return c.command(ctx, "Disconnect", Arguments{})
}

func (c *Client) Probe(ctx context.Context, sub, id string) error {
	return c.command(ctx, "Probe", Arguments{SubscriptionID: sub, NodeID: id})
}

func (c *Client) SetTun(ctx context.Context, enable bool) error {
	return c.command(ctx, "SetTun", Arguments{Tun: new(enable)})
}

func (c *Client) SetSettings(ctx context.Context, s domain.Settings) error {
	return c.command(ctx, "SetSettings", Arguments{Settings: s})
}

func (c *Client) SetAutostart(ctx context.Context, enabled bool) error {
	return c.command(ctx, "SetAutostart", Arguments{Autostart: enabled})
}

func (c *Client) SetCollapsed(ctx context.Context, id string, collapsed *bool) error {
	return c.command(ctx, "SetCollapsed", Arguments{SubscriptionID: id, Collapsed: collapsed})
}

func (c *Client) Shutdown(ctx context.Context) error { return c.command(ctx, "Shutdown", Arguments{}) }

func (c *Client) command(ctx context.Context, method string, args Arguments) error {
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

	if err := json.NewEncoder(conn).Encode(Request{Version: version.Version, Method: "Watch"}); err != nil {
		return fmt.Errorf("watch: %w", err)
	}
	dec := json.NewDecoder(conn)
	for {
		snap, err := receive[Snapshot](dec)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("watch: %w", err)
		}
		onUpdate(snap)
	}
}
