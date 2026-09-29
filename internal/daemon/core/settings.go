package core

import (
	"context"
	"errors"
	"slices"

	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/platform/autostart"
)

func (c *Core) SetSettings(ctx context.Context, settings domain.Settings) error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	settings, err := settings.Normalize()
	if err != nil {
		return err
	}
	next := c.current()
	if settings.Autostart != next.Settings.Autostart {
		return errors.New("autostart must be changed separately")
	}
	next.Settings = settings
	if err := c.store.SaveConfig(next.Settings); err != nil {
		return err
	}
	c.stMu.Lock()
	c.state.Settings = settings
	c.stMu.Unlock()
	applyErr := c.apply(ctx, next, c.conn.Status().Tun)
	c.publish()
	return applyErr
}

func (c *Core) SetAutostart(ctx context.Context, enabled bool) error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	actual := autostart.Enabled()
	var err error
	if actual != enabled {
		if enabled {
			err = autostart.Enable()
		} else {
			err = autostart.Disable()
		}
		actual = autostart.Enabled()
	}
	c.stMu.Lock()
	if actual {
		c.state.Settings.Autostart = "on"
	} else {
		c.state.Settings.Autostart = "off"
	}
	c.stMu.Unlock()
	c.publish()
	if err == nil && actual != enabled {
		return errors.New("autostart state did not change")
	}
	return err
}

func (c *Core) SetCollapsed(id string, collapsed *bool) error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
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
