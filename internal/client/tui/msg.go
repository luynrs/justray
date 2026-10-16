package tui

import (
	"context"
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/luynrs/justray/internal/ipc"
)

type completed struct {
	connection bool
	err        error
}

type started struct{ err error }

type settingsSaved struct{ err error }

type pushed struct {
	snapshot ipc.Snapshot
	live     bool
	err      error
}

func action(connection bool, start, fn func() error) tea.Cmd {
	return func() tea.Msg {
		return completed{connection: connection, err: request(start, fn)}
	}
}

func request(start, fn func() error) error {
	if err := fn(); start == nil || !errors.Is(err, ipc.ErrNoDaemon) && !errors.Is(err, ipc.ErrVersion) {
		return err
	}
	if err := start(); err != nil {
		return err
	}
	return fn()
}

type tick struct{}

func watch(ctx context.Context, c *ipc.Client, ch chan<- pushed) tea.Cmd {
	return func() tea.Msg {
		for ctx.Err() == nil {
			err := c.Watch(ctx, func(snapshot ipc.Snapshot) {
				select {
				case ch <- pushed{snapshot: snapshot, live: true}:
				case <-ctx.Done():
				}
			})
			if !errors.Is(err, ipc.ErrVersion) {
				err = nil
			}
			select {
			case ch <- pushed{err: err}:
			case <-ctx.Done():
				return nil
			}
			select {
			case <-time.After(50 * time.Millisecond):
			case <-ctx.Done():
				return nil
			}
		}
		return nil
	}
}

func next(ctx context.Context, ch <-chan pushed) tea.Cmd {
	return func() tea.Msg {
		select {
		case msg := <-ch:
			return msg
		case <-ctx.Done():
			return nil
		}
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return tick{} })
}
