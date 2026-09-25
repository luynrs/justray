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
	disk := Disk{Dir: t.TempDir()}
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
		Active: domain.NodeRef{SubscriptionID: "a", NodeID: "n1"},
		Last:   domain.NodeRef{SubscriptionID: "old", NodeID: "n0"},
		Tun:    true,
		Settings: domain.Settings{
			General: domain.General{RefreshEvery: 12},
			Routing: domain.Routing{Direct: []string{}, Proxy: []string{}, Block: []string{}},
		},
		Collapsed: []string{"a"},
	}
	if err := disk.Save(state); err != nil {
		t.Fatal(err)
	}
	rawState, err := os.ReadFile(ipc.State(disk.Dir))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rawState), "\"tls\"") || strings.Contains(string(rawState), "\"password\"") {
		t.Fatalf("expected empty fields to be omitted, got:\n%s", rawState)
	}
	rawConfig, err := os.ReadFile(ipc.Config(disk.Dir))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rawConfig), "\"refresh_hours\": 12") {
		t.Fatalf("expected settings in config.json, got:\n%s", rawConfig)
	}
	loadedState, err := disk.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loadedState, state) {
		t.Fatalf("got %+v, want %+v", loadedState, state)
	}
}
