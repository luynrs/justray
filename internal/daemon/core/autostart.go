package core

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/luynrs/justray/internal/platform/autostart"
)

type autostartCall struct {
	done  chan struct{}
	value string
	err   error
}

func (app *Core) readAutostart(ctx context.Context, enabled *bool) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if app.autostart != nil {
		return "", errors.New("autostart operation pending")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	call := &autostartCall{done: make(chan struct{})}
	app.autostart = call
	go func() {
		defer close(call.done)
		if call.err = ctx.Err(); call.err != nil {
			return
		}
		if enabled != nil {
			call.err = autostart.Set(ctx, *enabled)
			if call.err == nil {
				if *enabled {
					call.value = "on"
				} else {
					call.value = "off"
				}
			}
		} else {
			call.value, call.err = autostart.Current(ctx)
		}
	}()
	app.opMu.Unlock()
	select {
	case <-call.done:
		app.opMu.Lock()
		app.autostart = nil
		return call.value, call.err
	case <-ctx.Done():
		app.opMu.Lock()
		app.stMu.Lock()
		app.state.Settings.Autostart = ""
		if app.changing == nil {
			app.publishLocked()
		}
		app.stMu.Unlock()
		go func() {
			select {
			case <-app.ctx.Done():
				return
			case <-call.done:
			}
			app.opMu.Lock()
			defer app.opMu.Unlock()
			if app.ctx.Err() != nil {
				return
			}
			app.autostart = nil
			value, err := call.value, call.err
			if err != nil {
				value, err = app.readAutostart(app.ctx, nil)
			}
			if err != nil && app.ctx.Err() == nil {
				log.Print(err)
			}
			app.stMu.Lock()
			app.state.Settings.Autostart = value
			app.publishLocked()
			app.stMu.Unlock()
		}()
		return "", ctx.Err()
	}
}
