package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/luynrs/justray/internal/shared/rpc"
)

type loaded struct {
	snapshot rpc.Snapshot
	op       string
	err      error
}

type pushed struct {
	revision uint64
	live     bool
}

func snapshotCmd(op string, fn func() (rpc.Snapshot, error)) tea.Cmd {
	return func() tea.Msg {
		snapshot, err := fn()
		return loaded{snapshot: snapshot, op: op, err: err}
	}
}

type tick struct{}

func watch(ctx context.Context, c *rpc.Client, ch chan<- pushed) tea.Cmd {
	return func() tea.Msg {
		go func() {
			backoff := time.Second
			for {
				if ctx.Err() != nil {
					return
				}
				_ = c.Watch(ctx, func(changed rpc.Changed) {
					select {
					case ch <- pushed{revision: changed.Revision, live: true}:
					case <-ctx.Done():
					}
					backoff = time.Second
				})
				select {
				case ch <- pushed{}:
				case <-ctx.Done():
					return
				}
				select {
				case <-time.After(backoff):
				case <-ctx.Done():
					return
				}
				if backoff < 2*time.Second {
					backoff += 500 * time.Millisecond
				}
			}
		}()
		return nil
	}
}

func next(ch <-chan pushed) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return tick{} })
}
