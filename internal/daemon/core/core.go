package core

import (
	"context"
	"fmt"
	"log"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/luynrs/justray/internal/daemon/connection"
	"github.com/luynrs/justray/internal/daemon/store"
	"github.com/luynrs/justray/internal/daemon/subscription"
	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/engine"
	"github.com/luynrs/justray/internal/ipc"
)

type Core struct {
	// Lock order: opMu before stMu; no disk or engine I/O under stMu.
	opMu    sync.Mutex
	stMu    sync.Mutex
	ctx     context.Context
	cancel  context.CancelFunc
	store   store.Disk
	state   store.State
	probes  map[domain.NodeRef]engine.Result
	probing *probeCall
	conn    *connection.Service
	subs    *subscription.Service

	refreshes map[string]*refreshCall
	autostart *autostartCall
	changing  *changeCall

	snapshot atomic.Pointer[ipc.Snapshot]
	watchers map[chan ipc.Snapshot]struct{}
}

func New(ctx context.Context, st store.Disk, conn *connection.Service, subs *subscription.Service) (*Core, error) {
	if err := st.Migrate(); err != nil {
		return nil, fmt.Errorf("migrate data: %w", err)
	}
	state, err := st.Load()
	if err != nil {
		return nil, fmt.Errorf("load state: %w", err)
	}
	settings, err := state.Settings.Normalize()
	if err != nil {
		return nil, fmt.Errorf("normalize settings: %w", err)
	}
	state.Settings = settings
	ctx, cancel := context.WithCancel(ctx)
	c := &Core{
		ctx: ctx, cancel: cancel,
		store: st, state: state, conn: conn, subs: subs,
		probes:    map[domain.NodeRef]engine.Result{},
		refreshes: map[string]*refreshCall{},
		watchers:  map[chan ipc.Snapshot]struct{}{},
	}
	c.publish()
	c.opMu.Lock()
	value, err := c.readAutostart(ctx, nil)
	c.stMu.Lock()
	c.state.Settings.Autostart = value
	c.publishLocked()
	c.stMu.Unlock()
	c.opMu.Unlock()
	if err != nil {
		log.Print(err)
	}
	return c, nil
}

func (c *Core) Shutdown() {
	c.cancel()
	c.opMu.Lock()
	for c.changing != nil {
		finished := c.changing.done
		c.opMu.Unlock()
		<-finished
		c.opMu.Lock()
	}
	c.stMu.Lock()
	probe := c.probing
	c.stMu.Unlock()
	if probe != nil {
		<-probe.done
	}
	c.conn.Shutdown()
	c.publish()
	c.opMu.Unlock()
}

func (c *Core) current() store.State {
	c.stMu.Lock()
	defer c.stMu.Unlock()
	state := c.state
	state.Subscriptions = slices.Clone(state.Subscriptions)
	state.Collapsed = slices.Clone(state.Collapsed)
	state.Settings = state.Settings.Clone()
	return state
}

func (c *Core) commit(state store.State) error {
	if err := c.store.SaveState(state); err != nil {
		return err
	}
	c.stMu.Lock()
	state.Settings.Autostart = c.state.Settings.Autostart
	c.state = state
	c.stMu.Unlock()
	return nil
}
