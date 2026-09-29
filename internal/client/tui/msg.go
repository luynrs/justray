package tui

import (
	"context"
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/luynrs/justray/internal/ipc"
)

type completed struct {
	op  string
	err error
}

type pushed struct {
	snapshot ipc.Snapshot
	live     bool
	err      error
}

func (m Model) actionCmd(op string, start func(context.Context) error, fn func() error) tea.Cmd {
	return func() tea.Msg {
		if start != nil {
			if err := start(m.watch); err != nil {
				return completed{op: op, err: err}
			}
		}
		return completed{op: op, err: fn()}
	}
}

type tick struct{}

func watch(ctx context.Context, c *ipc.Client, ch chan<- pushed) tea.Cmd {
	return func() tea.Msg {
		for ctx.Err() == nil {
			err := c.Watch(ctx, func(snap ipc.Snapshot) {
				select {
				case ch <- pushed{snapshot: snap, live: true}:
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
			case <-time.After(time.Second):
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
