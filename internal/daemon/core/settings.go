package core

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/platform/autostart"
)

func (c *Core) SetSettings(ctx context.Context, expected, settings domain.Settings) error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	settings, err := settings.Normalize()
	if err != nil {
		return err
	}
	previous := c.current()
	previous.Settings.Autostart, err = autostart.Current(ctx)
	if err != nil {
		return err
	}
	if !expected.Equal(previous.Settings) {
		c.stMu.Lock()
		c.state.Settings.Autostart = previous.Settings.Autostart
		c.stMu.Unlock()
		c.publish()
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
		err = c.conn.Apply(ctx, node, status.NodeRef, settings, status.Tun)
	}
	if err == nil {
		if settings.Autostart != previous.Settings.Autostart {
			err = autostart.Set(ctx, settings.Autostart == "on")
		}
		if err == nil {
			err = ctx.Err()
		}
		if err == nil {
			err = c.store.SaveConfig(settings)
		}
		if err != nil && settings.Autostart != previous.Settings.Autostart {
			if rollbackErr := autostart.Set(context.WithoutCancel(ctx), previous.Settings.Autostart == "on"); rollbackErr != nil {
				var readErr error
				previous.Settings.Autostart, readErr = autostart.Current(context.WithoutCancel(ctx))
				err = errors.Join(err, fmt.Errorf("restore autostart: %w", rollbackErr), readErr)
			}
		}
	}
	if err != nil {
		if status.Connected {
			if rollbackErr := c.conn.Apply(context.WithoutCancel(ctx), node, status.NodeRef, previous.Settings, status.Tun); rollbackErr != nil {
				err = errors.Join(err, fmt.Errorf("restore settings: %w", rollbackErr))
			}
		}
		settings = previous.Settings
	}
	c.stMu.Lock()
	c.state.Settings = settings
	c.stMu.Unlock()
	c.publish()
	return err
}

func (c *Core) SetCollapsed(ctx context.Context, id string, collapsed *bool) error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	next := c.current()
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
