package store

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"time"

	"github.com/luynrs/justray/internal/daemon/platform/owner"
	"github.com/luynrs/justray/internal/shared/domain"
	"github.com/luynrs/justray/internal/shared/rpc"
	"gopkg.in/yaml.v3"
)

type Subscription struct {
	ID        string         `yaml:"id"`
	Name      string         `yaml:"name"`
	URL       string         `yaml:"url"`
	Nodes     []domain.Node  `yaml:"nodes"`
	UpdatedAt time.Time      `yaml:"updated_at"`
	Traffic   domain.Traffic `yaml:"traffic,omitempty"`
}

type PersistentState struct {
	Subscriptions []Subscription
	Active        domain.NodeRef
	Last          domain.NodeRef
	Tun           bool
	Settings      domain.Settings
}

// Disk reads and writes the daemon's persistent state.
type Disk struct{ Dir string }

type file struct {
	Subscriptions *[]Subscription `yaml:"subscriptions"`
	Active        string          `yaml:"active"`
	ActiveSub     string          `yaml:"active_subscription,omitempty"`
	Last          string          `yaml:"last,omitempty"`
	LastSub       string          `yaml:"last_subscription,omitempty"`
	Tun           bool            `yaml:"tun,omitempty"`
	Settings      domain.Settings `yaml:"settings,omitempty"`
}

func (d Disk) Load() (PersistentState, error) {
	state := PersistentState{Settings: domain.Settings{General: domain.General{RefreshEvery: domain.DefaultRefresh}}}
	data, err := os.ReadFile(rpc.Configuration(d.Dir))
	if err != nil {
		if err := skipMissing(err); err != nil {
			return state, err
		}
		return d.loadSubscriptions(state)
	}
	var f file
	if err := yaml.Unmarshal(data, &f); err != nil {
		return state, err
	}
	state.Active = domain.NodeRef{SubscriptionID: f.ActiveSub, NodeID: f.Active}
	state.Last = domain.NodeRef{SubscriptionID: f.LastSub, NodeID: f.Last}
	state.Tun, state.Settings = f.Tun, f.Settings
	if f.Subscriptions != nil {
		state.Subscriptions = *f.Subscriptions
		return state, nil
	}
	return d.loadSubscriptions(state)
}

func (d Disk) loadSubscriptions(state PersistentState) (PersistentState, error) {
	data, err := os.ReadFile(rpc.Subscriptions(d.Dir))
	if err != nil {
		return state, skipMissing(err)
	}
	var f struct {
		Subscriptions []Subscription `yaml:"subscriptions"`
	}
	if err := yaml.Unmarshal(data, &f); err != nil {
		return state, err
	}
	state.Subscriptions = f.Subscriptions
	return state, nil
}

func (d Disk) Save(state PersistentState) error {
	if state.Subscriptions == nil {
		state.Subscriptions = []Subscription{}
	}
	f := file{
		Subscriptions: &state.Subscriptions,
		Active:        state.Active.NodeID,
		ActiveSub:     state.Active.SubscriptionID,
		Last:          state.Last.NodeID,
		LastSub:       state.Last.SubscriptionID,
		Tun:           state.Tun,
		Settings:      state.Settings,
	}
	data, err := yaml.Marshal(f)
	if err != nil {
		return err
	}
	if err := write(rpc.Configuration(d.Dir), data); err != nil {
		return err
	}
	_ = os.Remove(rpc.Subscriptions(d.Dir))
	return nil
}

func skipMissing(err error) error {
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func write(path string, data []byte) error {
	tmp, err := os.OpenFile(path+".tmp", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // no-op once the rename below succeeds

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := owner.File(tmp.Name()); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	if dir, err := os.Open(filepath.Dir(path)); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

func NewID() string {
	var b [4]byte
	rand.Read(b[:]) // documented never to fail
	return hex.EncodeToString(b[:])
}
