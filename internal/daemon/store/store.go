package store

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"time"

	"github.com/luynrs/justray/internal/daemon/platform/owner"
	"github.com/luynrs/justray/internal/shared/domain"
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

type State struct {
	Active   string          `yaml:"active"`
	Last     string          `yaml:"last,omitempty"`
	Tun      bool            `yaml:"tun,omitempty"`
	Settings domain.Settings `yaml:"settings,omitempty"`
}

// Disk reads and writes subscriptions.yaml and configuration.yaml
type Disk struct{ Dir string }

type file struct {
	Subscriptions []Subscription `yaml:"subscriptions"`
}

func (d Disk) Subscriptions() ([]Subscription, error) {
	data, err := os.ReadFile(subsPath(d.Dir))
	if err != nil {
		return nil, skipMissing(err)
	}
	var f file
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	return f.Subscriptions, nil
}

func (d Disk) Save(subs []Subscription) error {
	data, err := yaml.Marshal(file{subs})
	if err != nil {
		return err
	}
	return write(subsPath(d.Dir), data)
}

func (d Disk) State() (State, error) {
	data, err := os.ReadFile(statePath(d.Dir))
	if err != nil {
		return State{Settings: domain.Settings{General: domain.General{RefreshEvery: domain.DefaultRefresh}}}, skipMissing(err)
	}
	var s State
	return s, yaml.Unmarshal(data, &s)
}

func (d Disk) Active() (string, error) {
	s, err := d.State()
	return s.Active, err
}

// SetActive persists the node to restore on start; connecting also records it as Last
func (d Disk) SetActive(id string) error {
	return d.update(func(s *State) {
		s.Active = id
		if id != "" {
			s.Last = id
		}
	})
}

func (d Disk) Last() (string, error) {
	s, err := d.State()
	return s.Last, err
}

func (d Disk) SetLast(id string) error {
	return d.update(func(s *State) { s.Last = id })
}

func (d Disk) SetTun(on bool) error {
	return d.update(func(s *State) { s.Tun = on })
}

func (d Disk) SetSettings(in domain.Settings) error {
	return d.update(func(s *State) { s.Settings = in })
}

func (d Disk) update(edit func(*State)) error {
	s, err := d.State()
	if err != nil {
		return err
	}
	edit(&s)
	data, err := yaml.Marshal(s)
	if err != nil {
		return err
	}
	return write(statePath(d.Dir), data)
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

func subsPath(dir string) string  { return filepath.Join(dir, "subscriptions.yaml") }
func statePath(dir string) string { return filepath.Join(dir, "configuration.yaml") }
