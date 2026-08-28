package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/luynrs/justray/internal/shared/domain"
	"github.com/luynrs/justray/internal/shared/rpc"
)

type loaded struct {
	subs  []rpc.Sub
	nodes []rpc.Node
	err   error
}

type pushed struct {
	st   rpc.Status
	live bool
}

type connectResult struct{ err error }

func connectCmd(fn func() error) tea.Cmd {
	return func() tea.Msg { return connectResult{fn()} }
}

type tick struct{}

func load(c *rpc.Client) tea.Msg {
	subs, err := c.Subs()
	if err != nil {
		return loaded{err: err}
	}
	nodes, err := c.Nodes()
	return loaded{subs: subs, nodes: nodes, err: err}
}

func act(c *rpc.Client, fn func() error) tea.Cmd {
	return func() tea.Msg {
		if err := fn(); err != nil {
			return loaded{err: err}
		}
		return load(c)
	}
}

func probeCmd(c *rpc.Client, sub, id string) tea.Cmd {
	return func() tea.Msg {
		nodes, err := c.Probe(sub, id)
		return loaded{nodes: nodes, err: err}
	}
}

func watch(ctx context.Context, c *rpc.Client, ch chan<- pushed) tea.Cmd {
	return func() tea.Msg {
		go func() {
			backoff := time.Second
			for {
				if ctx.Err() != nil {
					return
				}
				_ = c.Watch(ctx, func(st rpc.Status) {
					select {
					case ch <- pushed{st: st, live: true}:
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
				if backoff < 10*time.Second {
					backoff *= 2
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

type settingsLoaded struct {
	s    domain.Settings
	err  error
	open bool
}

func settingsCmd(c *rpc.Client, open bool) tea.Cmd {
	return func() tea.Msg {
		s, err := c.Settings()
		return settingsLoaded{s: s, err: err, open: open}
	}
}
