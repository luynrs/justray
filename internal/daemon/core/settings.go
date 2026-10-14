package core

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/luynrs/justray/internal/daemon/store"
	"github.com/luynrs/justray/internal/domain"
)

func (c *Core) SetSettings(ctx context.Context, expected, settings domain.Settings) error {
	return c.change(ctx, false, func(ctx context.Context) error {
		settings, err := settings.Normalize()
		if err != nil {
			return err
		}
		previous := c.current()
		if c.autostart == nil || expected.Autostart != "" || settings.Autostart != "" {
			previous.Settings.Autostart, err = c.readAutostart(ctx, nil)
			if err != nil {
				return err
			}
		}
		if settings.Autostart == "" {
			settings.Autostart = previous.Settings.Autostart
		}
		if !expected.Equal(previous.Settings) {
			c.stMu.Lock()
			c.state.Settings.Autostart = previous.Settings.Autostart
			c.stMu.Unlock()
			return errors.New("settings changed; reopen settings and try again")
		}
		if settings.Equal(previous.Settings) {
			return nil
		}
		status := c.conn.Status()
		var node domain.Node
		if status.Connected {
			node, _, err = find(previous.Subscriptions, status.NodeRef)
			if err != nil {
				return err
			}
			err = c.external(func() error { return c.conn.Apply(ctx, node, status.NodeRef, settings, status.Tun) })
		}
		if err == nil {
			if settings.Autostart != previous.Settings.Autostart {
				_, err = c.readAutostart(ctx, new(settings.Autostart == "on"))
			}
			if err == nil {
				err = ctx.Err()
			}
			if err == nil {
				err = c.store.SaveConfig(settings)
			}
			if err != nil && settings.Autostart != previous.Settings.Autostart {
				if c.autostart != nil {
					previous.Settings.Autostart = ""
				} else if _, rollbackErr := c.readAutostart(context.WithoutCancel(ctx), new(previous.Settings.Autostart == "on")); rollbackErr != nil {
					var readErr error
					previous.Settings.Autostart, readErr = c.readAutostart(context.WithoutCancel(ctx), nil)
					err = errors.Join(err, fmt.Errorf("restore autostart: %w", rollbackErr), readErr)
				}
			}
		}
		if err != nil {
			if status.Connected {
				if rollbackErr := c.external(func() error {
					return c.conn.Apply(context.WithoutCancel(ctx), node, status.NodeRef, previous.Settings, status.Tun)
				}); rollbackErr != nil {
					err = errors.Join(err, fmt.Errorf("restore settings: %w", rollbackErr))
				}
			}
			settings = previous.Settings
		}
		c.stMu.Lock()
		if settings.Autostart == "" {
			settings.Autostart = c.state.Settings.Autostart
		}
		c.state.Settings = settings
		c.stMu.Unlock()
		return err
	})
}

func (c *Core) SetCollapsed(ctx context.Context, id string, collapsed *bool) error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if err := c.ctx.Err(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	next := c.current()
	if !slices.ContainsFunc(next.Subscriptions, func(subscription store.Subscription) bool { return subscription.ID == id }) {
		return fmt.Errorf("subscription %q not found", id)
	}
	index := slices.Index(next.Collapsed, id)
	if collapsed != nil && (index >= 0) == *collapsed {
		return nil
	}
	if index < 0 {
		next.Collapsed = append(next.Collapsed, id)
	} else {
		next.Collapsed = slices.Delete(next.Collapsed, index, index+1)
	}
	if err := c.commit(next); err != nil {
		return err
	}
	c.publish()
	return nil
}
