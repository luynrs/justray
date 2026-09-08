package store

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/ipc"
)

func TestRoundtrip(t *testing.T) {
	d := Disk{Dir: t.TempDir()}
	state := PersistentState{
		Subscriptions: []Subscription{{
			ID: "a", Name: "test", URL: "https://example.com/sub",
			UpdatedAt: time.Now().Truncate(time.Second).UTC(),
			Traffic:   domain.Traffic{UploadBytes: 1, DownloadBytes: 2, TotalBytes: 3},
			Nodes: []domain.Node{{
				ID: "n1", Name: "node", Protocol: domain.VLess,
				Server: "1.2.3.4", Port: 443, Auth: domain.Auth{UUID: "uuid"},
			}},
		}},
		Active:   domain.NodeRef{SubscriptionID: "a", NodeID: "n1"},
		Last:     domain.NodeRef{SubscriptionID: "old", NodeID: "n0"},
		Tun:      true,
		Settings: domain.Settings{General: domain.General{RefreshEvery: 12}},
	}
	if err := d.Save(state); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(ipc.Configuration(d.Dir))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "tls:") || strings.Contains(string(raw), "password:") {
		t.Fatalf("expected empty fields to be omitted, got:\n%s", raw)
	}
	got, err := d.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, state) {
		t.Fatalf("got %+v, want %+v", got, state)
	}
}

func TestLoadMigrates(t *testing.T) {
	d := Disk{Dir: t.TempDir()}
	if err := os.WriteFile(ipc.Configuration(d.Dir), []byte("active: node\nactive_subscription: sub\nlast: old\nlast_subscription: old-sub\ntun: true\nsettings:\n  general:\n    refresh_hours: 12\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ipc.Subscriptions(d.Dir), []byte("subscriptions:\n  - id: sub\n    name: test\n    url: https://example.com/sub\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	state, err := d.Load()
	if err != nil {
		t.Fatal(err)
	}
	if state.Active != (domain.NodeRef{SubscriptionID: "sub", NodeID: "node"}) || state.Last != (domain.NodeRef{SubscriptionID: "old-sub", NodeID: "old"}) || !state.Tun || len(state.Subscriptions) != 1 {
		t.Fatalf("migration state = %+v", state)
	}
	if err := d.Save(state); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ipc.Subscriptions(d.Dir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy subscriptions file: %v", err)
	}
}

func TestEmptySnapshot(t *testing.T) {
	d := Disk{Dir: t.TempDir()}
	if err := d.Save(PersistentState{}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ipc.Subscriptions(d.Dir), []byte("subscriptions:\n  - id: legacy\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	state, err := d.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Subscriptions) != 0 {
		t.Fatalf("subscriptions = %+v, want empty snapshot", state.Subscriptions)
	}
}

func TestNewID(t *testing.T) {
	a, b := NewID(), NewID()
	if a == b {
		t.Fatalf("got same id %q", a)
	}
	if len(a) != 8 {
		t.Fatalf("len(%q) = %d, want 8", a, len(a))
	}
}
