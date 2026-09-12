// Deprecated: remove in 1.6.0

package store

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/luynrs/justray/internal/domain"
)

type legacyFile struct {
	Subscriptions []Subscription `yaml:"subscriptions"`
	Active        string         `yaml:"active"`
	ActiveSub     string         `yaml:"active_subscription"`
	Last          string         `yaml:"last"`
	LastSub       string         `yaml:"last_subscription"`
	Tun           bool           `yaml:"tun"`
	Settings      struct {
		General struct {
			RefreshEvery int    `yaml:"refresh_hours"`
			Port         int    `yaml:"port"`
			LogLevel     string `yaml:"log_level"`
			ProbeURL     string `yaml:"probe_url"`
			Emoji        string `yaml:"emoji"`
		} `yaml:"general"`
		Network struct {
			DNSHijack string `yaml:"dns_hijack"`
			DNS       string `yaml:"dns"`
			IPVersion string `yaml:"ip_version"`
			TunStack  string `yaml:"stack"`
			TunMTU    int    `yaml:"mtu"`
			TunStrict string `yaml:"strict_route"`
		} `yaml:"network"`
		Routing struct {
			Mode        string   `yaml:"mode"`
			BypassLocal string   `yaml:"bypass_local"`
			BlockQUIC   string   `yaml:"block_quic"`
			Except      []string `yaml:"except"`
			Blocked     []string `yaml:"blocked"`
		} `yaml:"routing"`
	} `yaml:"settings"`
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

	var f legacyFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return false, err
	}

	refresh := f.Settings.General.RefreshEvery
	if refresh == 0 {
		refresh = domain.DefaultRefresh
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
		settings.Routing.Proxy = f.Settings.Routing.Except
	} else {
		settings.Routing.Direct = f.Settings.Routing.Except
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
