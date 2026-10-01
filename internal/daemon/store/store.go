package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/ipc"
)

type Subscription struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	URL       string         `json:"url"`
	Nodes     []domain.Node  `json:"nodes"`
	UpdatedAt time.Time      `json:"updated_at"`
	Traffic   domain.Traffic `json:"traffic,omitempty"`
	Warning   string         `json:"warning,omitempty"`
}

type PersistentState struct {
	Subscriptions []Subscription  `json:"subscriptions"`
	Active        domain.NodeRef  `json:"active,omitzero"`
	Last          domain.NodeRef  `json:"last,omitzero"`
	Tun           bool            `json:"tun,omitempty"`
	Pending       *Pending        `json:"pending,omitempty"`
	Settings      domain.Settings `json:"-"`
	Collapsed     []string        `json:"collapsed,omitempty"`
}

type Pending struct {
	Ref domain.NodeRef `json:"ref"`
	Tun bool           `json:"tun"`
}

// Disk reads and writes the daemon's persistent state.
type Disk struct{ Dir string }

func (d Disk) Load() (PersistentState, error) {
	state := PersistentState{
		Settings:      domain.Settings{General: domain.General{RefreshEvery: domain.DefaultRefresh}},
		Subscriptions: []Subscription{},
	}
	cfgData, cfgErr := os.ReadFile(ipc.Config(d.Dir))
	stateData, stateErr := os.ReadFile(ipc.State(d.Dir))

	if cfgErr == nil {
		if err := json.Unmarshal(cfgData, &state.Settings); err != nil {
			return state, err
		}
	} else if !os.IsNotExist(cfgErr) {
		return state, cfgErr
	}

	if stateErr == nil {
		if err := json.Unmarshal(stateData, &state); err != nil {
			return state, err
		}
		if state.Subscriptions == nil {
			state.Subscriptions = []Subscription{}
		}
	} else if !os.IsNotExist(stateErr) {
		return state, stateErr
	}

	return state, nil
}

func (d Disk) SaveState(state PersistentState) error {
	if state.Subscriptions == nil {
		state.Subscriptions = []Subscription{}
	}
	stateData, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	stateData = append(stateData, '\n')
	return write(ipc.State(d.Dir), stateData)
}

func (d Disk) SaveConfig(settings domain.Settings) error {
	settings.Autostart = ""
	for _, l := range []*[]string{&settings.Direct, &settings.Proxy, &settings.Block} {
		if *l == nil {
			*l = []string{}
		}
	}
	cfgData, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	cfgData = append(cfgData, '\n')
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
	if err := ipc.Chown(tmp.Name()); err != nil {
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

func NewNodeID() string {
	var b [16]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
