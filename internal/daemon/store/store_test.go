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
		Active:   domain.NodeRef{SubscriptionID: "a", NodeID: "n1"},
		Last:     domain.NodeRef{SubscriptionID: "old", NodeID: "n0"},
		Tun:      true,
		Settings: domain.Settings{
			General: domain.General{RefreshEvery: 12},
			Routing: domain.Routing{Direct: []string{}, Proxy: []string{}, Block: []string{}},
		},
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

func TestMigrateLegacy(t *testing.T) {
	dir := t.TempDir()
	legacyYAML := `
active: "n1"
active_subscription: "sub1"
tun: true
settings:
  general:
    refresh_hours: 6
    log_level: "debug"
  network:
    dns_hijack: "on"
    ip_version: "auto"
  routing:
    mode: "direct all"
    except: ["1.1.1.1"]
subscriptions:
  - id: "sub1"
    name: "My Sub"
    url: "https://example.com"
    traffic:
      upload_bytes: 100
      download_bytes: 200
    nodes:
      - id: "n1"
        name: "Reality Node"
        protocol: "vless"
        server: "example.com"
        port: 443
        packet_encoding: "packetaddr"
        reality:
          public_key: "pubkey"
          short_id: "shortid"
        wireguard:
          private_key: "privkey"
          peer_public_key: "peerkey"
`
	if err := os.WriteFile(dir+"/configuration.yaml", []byte(legacyYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	d := Disk{Dir: dir}
	state, err := d.Load()
	if err != nil {
		t.Fatalf("Load with legacy configuration failed: %v", err)
	}
	if state.Active.NodeID != "n1" || state.Active.SubscriptionID != "sub1" || !state.Tun {
		t.Fatalf("active state mismatch: %+v", state)
	}
	if state.Settings.RefreshEvery != 6 || state.Settings.LogLevel != "debug" || state.Settings.Mode != domain.DirectAll {
		t.Fatalf("settings mismatch: %+v", state.Settings)
	}
	if len(state.Settings.Proxy) != 1 || state.Settings.Proxy[0] != "1.1.1.1" {
		t.Fatalf("proxy routing mismatch: %+v", state.Settings.Proxy)
	}
	if len(state.Subscriptions) != 1 {
		t.Fatalf("subscriptions len = %d, want 1", len(state.Subscriptions))
	}
	sub := state.Subscriptions[0]
	if sub.Traffic.UploadBytes != 100 || sub.Traffic.DownloadBytes != 200 {
		t.Fatalf("traffic mismatch: %+v", sub.Traffic)
	}
	if len(sub.Nodes) != 1 {
		t.Fatalf("nodes len = %d, want 1", len(sub.Nodes))
	}
	node := sub.Nodes[0]
	if node.PacketEncoding != "packetaddr" {
		t.Fatalf("PacketEncoding = %q, want packetaddr", node.PacketEncoding)
	}
	if node.Reality == nil || node.Reality.PublicKey != "pubkey" || node.Reality.ShortID != "shortid" {
		t.Fatalf("Reality mismatch: %+v", node.Reality)
	}
	if node.WireGuard == nil || node.WireGuard.PrivateKey != "privkey" || node.WireGuard.PeerPublicKey != "peerkey" {
		t.Fatalf("WireGuard mismatch: %+v", node.WireGuard)
	}
	if _, err := os.Stat(dir + "/configuration.yaml"); !os.IsNotExist(err) {
		t.Fatal("expected configuration.yaml to be removed after migration")
	}
}
