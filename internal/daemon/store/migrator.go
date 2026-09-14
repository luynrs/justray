// Deprecated: remove in 1.6.0

package store

import (
	"encoding/json"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/luynrs/justray/internal/domain"
)

type legacyFile struct {
	Subscriptions []Subscription `json:"subscriptions"`
	Active        string         `json:"active"`
	ActiveSub     string         `json:"active_subscription"`
	Last          string         `json:"last"`
	LastSub       string         `json:"last_subscription"`
	Tun           bool           `json:"tun"`
	Settings      struct {
		General struct {
			RefreshEvery *int   `json:"refresh_hours"`
			Port         int    `json:"port"`
			LogLevel     string `json:"log_level"`
			ProbeURL     string `json:"probe_url"`
			Emoji        string `json:"emoji"`
		} `json:"general"`
		Network struct {
			DNSHijack string `json:"dns_hijack"`
			DNS       string `json:"dns"`
			IPVersion string `json:"ip_version"`
			TunStack  string `json:"stack"`
			TunMTU    int    `json:"mtu"`
			TunStrict string `json:"strict_route"`
		} `json:"network"`
		Routing struct {
			Mode        string   `json:"mode"`
			BypassLocal string   `json:"bypass_local"`
			BlockQUIC   string   `json:"block_quic"`
			Except      []string `json:"except"`
			Blocked     []string `json:"blocked"`
		} `json:"routing"`
	} `json:"settings"`
}

func (d Disk) migrateLegacy() (bool, error) {
	path := filepath.Join(d.Dir, "configuration.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	var raw any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return false, err
	}
	jsonData, err := json.Marshal(raw)
	if err != nil {
		return false, err
	}

	var f legacyFile
	if err := json.Unmarshal(jsonData, &f); err != nil {
		return false, err
	}

	refresh := domain.DefaultRefresh
	if f.Settings.General.RefreshEvery != nil {
		refresh = *f.Settings.General.RefreshEvery
	}

	settings := domain.Settings{
		General: domain.General{
			RefreshEvery: refresh,
			LogLevel:     f.Settings.General.LogLevel,
			ProbeURL:     f.Settings.General.ProbeURL,
			Emoji:        f.Settings.General.Emoji,
		},
		Connection: domain.Connection{
			Port:      f.Settings.General.Port,
			DNSHijack: f.Settings.Network.DNSHijack,
			DNS:       f.Settings.Network.DNS,
			IPVersion: f.Settings.Network.IPVersion,
			TunStack:  f.Settings.Network.TunStack,
			TunMTU:    f.Settings.Network.TunMTU,
		},
		Routing: domain.Routing{
			Mode:        f.Settings.Routing.Mode,
			BypassLocal: f.Settings.Routing.BypassLocal,
			TunStrict:   f.Settings.Network.TunStrict,
			BlockQUIC:   f.Settings.Routing.BlockQUIC,
			Block:       f.Settings.Routing.Blocked,
		},
	}
	if f.Settings.Routing.Mode == domain.DirectAll {
		settings.Proxy = f.Settings.Routing.Except
	} else {
		settings.Direct = f.Settings.Routing.Except
	}

	subs := f.Subscriptions
	if subs == nil {
		subs = []Subscription{}
	}

	err = d.Save(PersistentState{
		Subscriptions: subs,
		Active:        domain.NodeRef{SubscriptionID: f.ActiveSub, NodeID: f.Active},
		Last:          domain.NodeRef{SubscriptionID: f.LastSub, NodeID: f.Last},
		Tun:           f.Tun,
		Settings:      settings,
	})
	if err != nil {
		return false, err
	}

	_ = os.Remove(path)
	return true, nil
}
