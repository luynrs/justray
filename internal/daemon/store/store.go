package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	Traffic   domain.Traffic `json:"traffic,omitzero"`
	Warning   string         `json:"warning,omitempty"`
}

type State struct {
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

func (d Disk) Load() (State, error) {
	state := State{
		Settings:      domain.Settings{General: domain.General{RefreshEvery: domain.DefaultRefresh}},
		Subscriptions: []Subscription{},
	}
	cfgData, cfgErr := os.ReadFile(ipc.Config(d.Dir))
	stateData, stateErr := os.ReadFile(ipc.State(d.Dir))

	if cfgErr == nil {
		if err := json.Unmarshal(cfgData, &state.Settings); err != nil {
			return state, err
		}
	} else if !errors.Is(cfgErr, os.ErrNotExist) {
		return state, cfgErr
	}

	if stateErr == nil {
		if err := json.Unmarshal(stateData, &state); err != nil {
			return state, err
		}
		if state.Subscriptions == nil {
			state.Subscriptions = []Subscription{}
		}
	} else if !errors.Is(stateErr, os.ErrNotExist) {
		return state, stateErr
	}

	return state, nil
}

func (d Disk) SaveState(state State) error {
	if state.Subscriptions == nil {
		state.Subscriptions = []Subscription{}
	}
	return writeJSON(ipc.State(d.Dir), state)
}

func (d Disk) SaveConfig(settings domain.Settings) error {
	settings.Autostart = ""
	for _, l := range []*[]string{&settings.Direct, &settings.Proxy, &settings.Block} {
		if *l == nil {
			*l = []string{}
		}
	}
	return writeJSON(ipc.Config(d.Dir), settings)
}

func writeJSON(path string, value any) (err error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.OpenFile(path+".lock", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		_ = tmp.Close()
		if err != nil {
			_ = os.Remove(tmp.Name())
		}
	}() // no-op once the rename below succeeds

	if _, err := tmp.Write(append(data, '\n')); err != nil {
		return err
	}
	if err := ipc.Chown(tmp.Name(), tmp); err != nil {
		return err
	}
	if err := errors.Join(tmp.Sync(), tmp.Close()); err != nil {
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

func NewID() string     { return newID(4) }
func NewNodeID() string { return newID(16) }

func newID(size int) string {
	var random [16]byte
	rand.Read(random[:size]) // documented never to fail
	return hex.EncodeToString(random[:size])
}
