package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/ipc"
	"github.com/luynrs/justray/internal/parser"
)

type Subscription struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	URL       string         `json:"url"`
	Nodes     []domain.Node  `json:"nodes"`
	UpdatedAt time.Time      `json:"updated_at"`
	Traffic   domain.Traffic `json:"traffic,omitempty"`
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
	Subscriptions []Subscription `json:"subscriptions"`
	Active        string         `json:"active,omitempty"`
	ActiveSub     string         `json:"active_subscription,omitempty"`
	Last          string         `json:"last,omitempty"`
	LastSub       string         `json:"last_subscription,omitempty"`
	Tun           bool           `json:"tun,omitempty"`
	Collapsed     []string       `json:"collapsed,omitempty"`
}

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
		var sf stateFile
		if err := json.Unmarshal(stateData, &sf); err != nil {
			return state, err
		}
		state.Active = domain.NodeRef{SubscriptionID: sf.ActiveSub, NodeID: sf.Active}
		state.Last = domain.NodeRef{SubscriptionID: sf.LastSub, NodeID: sf.Last}
		state.Tun = sf.Tun
		state.Collapsed = sf.Collapsed
		if sf.Subscriptions != nil {
			state.Subscriptions = sf.Subscriptions
		}
		var directNodes []domain.Node
		position := -1
		subs := make([]Subscription, 0, len(state.Subscriptions))
		for _, sub := range state.Subscriptions {
			if parser.IsLink(sub.URL) {
				if position < 0 {
					position = len(subs)
				}
				for _, node := range sub.Nodes {
					if !slices.ContainsFunc(directNodes, func(n domain.Node) bool { return n.ID == node.ID }) {
						directNodes = append(directNodes, node)
					}
				}
				if state.Active.SubscriptionID == sub.ID || (state.Active.SubscriptionID == "" && slices.ContainsFunc(sub.Nodes, func(n domain.Node) bool { return n.ID == state.Active.NodeID })) {
					state.Active.SubscriptionID = "default"
				}
				if state.Last.SubscriptionID == sub.ID || (state.Last.SubscriptionID == "" && slices.ContainsFunc(sub.Nodes, func(n domain.Node) bool { return n.ID == state.Last.NodeID })) {
					state.Last.SubscriptionID = "default"
				}
				continue
			}
			subs = append(subs, sub)
		}
		if position >= 0 {
			if index := slices.IndexFunc(subs, func(sub Subscription) bool { return sub.ID == "default" }); index >= 0 {
				for _, node := range directNodes {
					if !slices.ContainsFunc(subs[index].Nodes, func(n domain.Node) bool { return n.ID == node.ID }) {
						subs[index].Nodes = append(subs[index].Nodes, node)
					}
				}
			} else {
				subs = slices.Insert(subs, position, Subscription{ID: "default", Name: "Default", Nodes: directNodes})
			}
		}
		state.Subscriptions = subs
	} else if !os.IsNotExist(stateErr) {
		return state, stateErr
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
	stateData, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return err
	}
	stateData = append(stateData, '\n')
	return write(ipc.State(d.Dir), stateData)
}

func (d Disk) SaveConfig(settings domain.Settings) error {
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
