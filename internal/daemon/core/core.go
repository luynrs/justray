package core

import (
	"fmt"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/luynrs/justray/internal/daemon/connection"
	"github.com/luynrs/justray/internal/daemon/store"
	"github.com/luynrs/justray/internal/daemon/subscription"
	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/engine"
	"github.com/luynrs/justray/internal/ipc"
	"github.com/luynrs/justray/internal/platform/autostart"
)

type Core struct {
	opMu    sync.Mutex
	stateMu sync.RWMutex
	store   store.Disk
	state   store.PersistentState
	probeMu sync.Mutex
	probes  map[domain.NodeRef]engine.Result
	probing map[domain.NodeRef]bool
	conn    *connection.Service
	subs    *subscription.Service

	refreshes map[string]*refreshCall

	snapshot atomic.Pointer[ipc.Snapshot]
	watchers map[chan ipc.Snapshot]struct{}
	pubMu    sync.Mutex
}

func New(st store.Disk, conn *connection.Service, subs *subscription.Service) (*Core, error) {
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
		probing:   map[domain.NodeRef]bool{},
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

func (c *Core) current() store.PersistentState {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	state := c.state
	state.Subscriptions = slices.Clone(state.Subscriptions)
	state.Collapsed = slices.Clone(state.Collapsed)
	state.Settings = cloneSettings(state.Settings)
	return state
}

func (c *Core) commit(state store.PersistentState) error {
	if err := c.store.SaveState(state); err != nil {
		return err
	}
	c.stateMu.Lock()
	c.state = state
	c.stateMu.Unlock()
	return nil
}
