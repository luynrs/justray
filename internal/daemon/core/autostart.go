package core

import (
	"context"
	"time"

	"github.com/luynrs/justray/internal/platform/autostart"
)

func (app *Core) readAutostart(ctx context.Context, enabled *bool) (value string, err error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	err = app.external(func() error {
		if enabled == nil {
			value, err = autostart.Current(ctx)
			return err
		}
		if err = autostart.Set(ctx, *enabled); err == nil {
			if *enabled {
				value = "on"
			} else {
				value = "off"
			}
		}
		return err
	})
	return value, err
}
