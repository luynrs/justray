package store

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"time"

	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/ipc"
	"github.com/luynrs/justray/internal/platform/owner"
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
	Collapsed     []string
}

// Disk reads and writes the daemon's persistent state.
type Disk struct{ Dir string }

type stateFile struct {
	Subscriptions []Subscription `yaml:"subscriptions"`
	Active        string         `yaml:"active,omitempty"`
	ActiveSub     string         `yaml:"active_subscription,omitempty"`
	Last          string         `yaml:"last,omitempty"`
	LastSub       string         `yaml:"last_subscription,omitempty"`
	Tun           bool           `yaml:"tun,omitempty"`
	Collapsed     []string       `yaml:"collapsed,omitempty"`
}

func (d Disk) Load() (PersistentState, error) {
	state := PersistentState{
		Settings:      domain.Settings{General: domain.General{RefreshEvery: domain.DefaultRefresh}},
		Subscriptions: []Subscription{},
	}
	if cfgData, err := os.ReadFile(ipc.Config(d.Dir)); err == nil {
		if err := yaml.Unmarshal(cfgData, &state.Settings); err != nil {
			return state, err
		}
	} else if !os.IsNotExist(err) {
		return state, err
	}

	if stateData, err := os.ReadFile(ipc.State(d.Dir)); err == nil {
		var sf stateFile
		if err := yaml.Unmarshal(stateData, &sf); err != nil {
			return state, err
		}
		state.Active = domain.NodeRef{SubscriptionID: sf.ActiveSub, NodeID: sf.Active}
		state.Last = domain.NodeRef{SubscriptionID: sf.LastSub, NodeID: sf.Last}
		state.Tun = sf.Tun
		state.Collapsed = sf.Collapsed
		if sf.Subscriptions != nil {
			state.Subscriptions = sf.Subscriptions
		}
	} else if !os.IsNotExist(err) {
		return state, err
	}

	return state, nil
}

func (d Disk) Save(state PersistentState) error {
	if err := d.SaveState(state); err != nil {
		return err
	}
	return d.SaveConfig(state.Settings)
}

func (d Disk) SaveState(state PersistentState) error {
	if state.Subscriptions == nil {
		state.Subscriptions = []Subscription{}
	}
	sf := stateFile{
		Subscriptions: state.Subscriptions,
		Active:        state.Active.NodeID,
		ActiveSub:     state.Active.SubscriptionID,
		Last:          state.Last.NodeID,
		LastSub:       state.Last.SubscriptionID,
		Tun:           state.Tun,
		Collapsed:     state.Collapsed,
	}
	stateData, err := yaml.Marshal(sf)
	if err != nil {
		return err
	}
	return write(ipc.State(d.Dir), stateData)
}

func (d Disk) SaveConfig(settings domain.Settings) error {
	cfgData, err := yaml.Marshal(settings)
	if err != nil {
		return err
	}
	return write(ipc.Config(d.Dir), cfgData)
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
