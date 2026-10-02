package core

import (
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/luynrs/justray/internal/daemon/connection"
	"github.com/luynrs/justray/internal/daemon/store"
	"github.com/luynrs/justray/internal/daemon/subscription"
	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/engine"
	"github.com/luynrs/justray/internal/ipc"
	"github.com/luynrs/justray/internal/platform/autostart"
)

type Core struct {
	// Lock order: opMu before stMu; no disk or engine I/O under stMu.
	opMu    sync.Mutex
	stMu    sync.Mutex
	store   store.Disk
	state   store.State
	probes  map[domain.NodeRef]engine.Result
	probing map[domain.NodeRef]*probeCall
	conn    *connection.Service
	subs    *subscription.Service

	refreshes  map[string]*refreshCall
	probeTimer *time.Timer

	snapshot atomic.Pointer[ipc.Snapshot]
	watchers map[chan ipc.Snapshot]struct{}
}

func New(st store.Disk, conn *connection.Service, subs *subscription.Service) (*Core, error) {
	if err := st.Migrate(); err != nil {
		return nil, fmt.Errorf("migrate data: %w", err)
	}
	state, err := st.Load()
	if err != nil {
		return nil, fmt.Errorf("load state: %w", err)
	}
	state.Settings.Autostart = "off"
	if autostart.Enabled() {
		state.Settings.Autostart = "on"
	}
	settings, err := state.Settings.Normalize()
	if err != nil {
		return nil, fmt.Errorf("normalize settings: %w", err)
	}
	state.Settings = settings
	c := &Core{
		store: st, state: state, conn: conn, subs: subs,
		probes:    map[domain.NodeRef]engine.Result{},
		probing:   map[domain.NodeRef]*probeCall{},
		refreshes: map[string]*refreshCall{},
		watchers:  map[chan ipc.Snapshot]struct{}{},
	}
	c.publish()
	return c, nil
}

func (c *Core) Shutdown() {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	c.conn.Shutdown()
	c.publish()
}

func (c *Core) current() store.State {
	c.stMu.Lock()
	defer c.stMu.Unlock()
	state := c.state
	state.Subscriptions = slices.Clone(state.Subscriptions)
	state.Collapsed = slices.Clone(state.Collapsed)
	state.Settings = cloneSettings(state.Settings)
	return state
}

func (c *Core) commit(state store.State) error {
	if err := c.store.SaveState(state); err != nil {
		return err
	}
	c.stMu.Lock()
	c.state = state
	c.stMu.Unlock()
	return nil
}
