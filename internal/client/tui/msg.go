package tui

import (
	"context"
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
}

func actionCmd(op string, fn func() error) tea.Cmd {
	return func() tea.Msg {
		return completed{op: op, err: fn()}
	}
}

type tick struct{}

func watch(ctx context.Context, c *ipc.Client, ch chan<- pushed) tea.Cmd {
	return func() tea.Msg {
		for ctx.Err() == nil {
			_ = c.Watch(ctx, func(snap ipc.Snapshot) {
				select {
				case ch <- pushed{snapshot: snap, live: true}:
				case <-ctx.Done():
				}
			})
			select {
			case ch <- pushed{}:
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
