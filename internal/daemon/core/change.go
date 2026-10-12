package core

import (
	"context"
	"errors"
	"time"

	"github.com/luynrs/justray/internal/daemon/store"
	"github.com/luynrs/justray/internal/ipc"
)

type changeCall struct {
	done   chan struct{}
	before ipc.Status
	state  store.State
	err    error
}

var errChanging = errors.New("state change still running; try again after it finishes")

func (app *Core) change(ctx context.Context, wait bool, action func(context.Context) error) error {
	ctx, cancel := app.requestContext(ctx)
	defer cancel()
	call, err := app.startChange(ctx, wait, action)
	if err != nil {
		return err
	}
	select {
	case <-call.done:
		return call.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (app *Core) requestContext(ctx context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	stop := context.AfterFunc(app.ctx, cancel)
	if app.ctx.Err() != nil {
		cancel()
	}
	return ctx, func() {
		stop()
		cancel()
	}
}

func (app *Core) startChange(ctx context.Context, wait bool, action func(context.Context) error) (*changeCall, error) {
	for {
		app.opMu.Lock()
		if err := ctx.Err(); err != nil {
			app.opMu.Unlock()
			return nil, err
		}
		if err := app.ctx.Err(); err != nil {
			app.opMu.Unlock()
			return nil, err
		}
		if app.changing == nil {
			break
		}
		done := app.changing.done
		app.opMu.Unlock()
		if !wait {
			return nil, errChanging
		}
		select {
		case <-done:
		case <-ctx.Done():
		}
	}
	call := &changeCall{done: make(chan struct{}), before: app.conn.Status(), state: app.current()}
	app.stMu.Lock()
	app.changing = call
	app.stMu.Unlock()
	app.opMu.Unlock()
	timer := time.AfterFunc(50*time.Millisecond, func() {
		app.stMu.Lock()
		defer app.stMu.Unlock()
		if app.changing == call {
			app.publishLocked()
		}
	})
	go func() {
		app.opMu.Lock()
		if call.err = app.ctx.Err(); call.err == nil {
			call.err = ctx.Err()
		}
		if call.err == nil {
			call.err = action(ctx)
		}
		timer.Stop()
		app.stMu.Lock()
		app.changing = nil
		app.publishLocked()
		app.stMu.Unlock()
		close(call.done)
		app.opMu.Unlock()
	}()
	return call, nil
}

func (app *Core) external(action func() error) error {
	app.opMu.Unlock()
	err := action()
	app.opMu.Lock()
	return err
}
