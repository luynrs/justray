package store

import (
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
		Active:    domain.NodeRef{SubscriptionID: "a", NodeID: "n1"},
		Last:      domain.NodeRef{SubscriptionID: "old", NodeID: "n0"},
		Tun:       true,
		Settings:  domain.Settings{General: domain.General{RefreshEvery: 12}},
		Collapsed: []string{"a"},
	}
	if err := d.Save(state); err != nil {
		t.Fatal(err)
	}
	rawState, err := os.ReadFile(ipc.State(d.Dir))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rawState), "\"tls\"") || strings.Contains(string(rawState), "\"password\"") {
		t.Fatalf("expected empty fields to be omitted, got:\n%s", rawState)
	}
	rawConfig, err := os.ReadFile(ipc.Config(d.Dir))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rawConfig), "\"refresh_hours\": 12") {
		t.Fatalf("expected settings in config.json, got:\n%s", rawConfig)
	}
	got, err := d.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, state) {
		t.Fatalf("got %+v, want %+v", got, state)
	}
}

func TestEmptySnapshot(t *testing.T) {
	d := Disk{Dir: t.TempDir()}
	if err := d.Save(PersistentState{}); err != nil {
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
