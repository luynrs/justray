package server

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/luynrs/justray/internal/daemon/connection"
	"github.com/luynrs/justray/internal/daemon/core"
	"github.com/luynrs/justray/internal/daemon/store"
	"github.com/luynrs/justray/internal/daemon/subscription"
	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/engine"
	"github.com/luynrs/justray/internal/ipc"
	"github.com/luynrs/justray/internal/platform/elevate"
	"github.com/luynrs/justray/internal/version"
)

type switchEngine struct {
	running bool
	failB   error
	failTun error
}

func (instance *switchEngine) Apply(_ context.Context, spec engine.Spec) error {
	if spec.Node.ID == "b" && instance.failB != nil {
		instance.running = false
		return instance.failB
	}
	if spec.Tun && instance.failTun != nil {
		instance.running = false
		return instance.failTun
	}
	instance.running = true
	return nil
}
func (instance *switchEngine) Stop() error   { instance.running = false; return nil }
func (instance *switchEngine) Running() bool { return instance.running }

func (*switchEngine) Probe(context.Context, []domain.Node, domain.Settings, func(int, engine.Result)) error {
	return errors.New("unexpected probe")
}

func requireNoError(test *testing.T, err error) {
	test.Helper()
	if err != nil {
		test.Fatal(err)
	}
}

func TestWatchLifecycle(t *testing.T) {
	directory := t.TempDir()
	requireNoError(t, ipc.EnsureDir(directory))
	listener, err := net.Listen("unix", filepath.Join(directory, "daemon.sock"))
	requireNoError(t, err)
	app, err := core.New(t.Context(), store.Disk{Dir: directory}, connection.New(t.Context(), directory, engine.New), subscription.New(t.Context()))
	requireNoError(t, err)
	server := New(t.Context(), app)
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()

	client := ipc.New(listener.Addr().String())
	updates := make(chan ipc.Snapshot, 8)
	watchDone := make(chan error, 1)
	go func() { watchDone <- client.Watch(t.Context(), func(snapshot ipc.Snapshot) { updates <- snapshot }) }()
	var snapshot ipc.Snapshot
	readSnapshot := func() error {
		timer := time.NewTimer(3 * time.Second)
		defer timer.Stop()
		for {
			select {
			case snapshot = <-updates:
				if !snapshot.Status.Busy {
					return nil
				}
			case err := <-watchDone:
				return err
			case <-timer.C:
				return errors.New("watch did not deliver completed snapshot")
			}
		}
	}
	if err := readSnapshot(); err != nil || snapshot.Settings.Port != domain.DefaultPort || snapshot.Settings.ProxyType != "local" {
		t.Fatalf("initial snapshot: %+v, %v", snapshot, err)
	}
	if snapshot.Settings.Details != (domain.Details{Address: "on", Transport: "off", Protocol: "on"}) {
		t.Fatalf("default node details: %+v", snapshot.Settings.Details)
	}
	for _, ver := range []string{"", version.Version + "-other"} {
		conn, err := net.Dial("unix", listener.Addr().String())
		requireNoError(t, err)
		_ = conn.SetDeadline(time.Now().Add(time.Second))
		err = json.NewEncoder(conn).Encode(ipc.Request{Version: ver, Method: "SetTun", Arguments: ipc.Arguments{Tun: new(true)}})
		if err != nil {
			_ = conn.Close()
			t.Fatal(err)
		}
		var res ipc.Response
		err = json.NewDecoder(conn).Decode(&res)
		_ = conn.Close()
		if err != nil || res.Success || res.Version != version.Version || res.Error == nil || !errors.Is(res.Error, ipc.ErrVersion) {
			t.Fatalf("version %q: %+v, %v", ver, res, err)
		}
	}
	if snap, err := client.Snapshot(t.Context()); err != nil || snap.Status.Tun {
		t.Fatalf("rejected request changed state: %+v, %v", snap, err)
	}

	link := "vless://11111111-1111-1111-1111-111111111111@127.0.0.1:443?security=tls&type=xhttp&extra=" + url.QueryEscape(`{"serverMaxHeaderBytes":16384,"sessionIDPlacement":"query"}`)
	added, err := client.AddSubscription(t.Context(), link)
	if err != nil || added.SubscriptionID == "" || added.NodeCount != 1 {
		t.Fatalf("added subscription: %+v, %v", added, err)
	}
	if err := readSnapshot(); err != nil || len(snapshot.Nodes) != 1 || snapshot.Nodes[0].SubscriptionID != added.SubscriptionID || snapshot.Nodes[0].Transport != "xhttp" {
		t.Fatalf("subscription snapshot: %+v, %v", snapshot, err)
	}
	for _, count := range []int{1, 0} {
		requireNoError(t, client.SetCollapsed(t.Context(), added.SubscriptionID, nil))
		if err := readSnapshot(); err != nil || len(snapshot.Collapsed) != count {
			t.Fatalf("collapse toggle: %v, %v", snapshot.Collapsed, err)
		}
	}
	if _, err := client.AddSubscription(t.Context(), link); err != nil {
		t.Fatal(err)
	}
	if err := readSnapshot(); err != nil || len(snapshot.Nodes) != 2 || snapshot.Nodes[0].NodeID == snapshot.Nodes[1].NodeID {
		t.Fatalf("duplicate link snapshot: %+v, %v", snapshot, err)
	}
	requireNoError(t, client.SetTun(t.Context(), true))
	if err := readSnapshot(); err != nil || !snapshot.Status.Tun {
		t.Fatalf("TUN snapshot: %+v, %v", snapshot, err)
	}
	settings := snapshot.Settings
	settings.DNS = "1.1.1.1"
	settings.Details = domain.Details{Address: "off", Transport: "on", Protocol: "off"}
	requireNoError(t, client.SetSettings(t.Context(), snapshot.Settings, settings))
	if err := readSnapshot(); err != nil || !snapshot.Settings.Equal(settings) {
		t.Fatalf("saved settings snapshot: %+v, %v", snapshot.Settings, err)
	}
	for _, invalid := range []domain.Details{
		{Address: "invalid", Transport: "on", Protocol: "off"},
		{Address: "off", Transport: "invalid", Protocol: "off"},
		{Address: "off", Transport: "on", Protocol: "invalid"},
	} {
		settings.Details = invalid
		if err := client.SetSettings(t.Context(), snapshot.Settings, settings); err == nil {
			t.Fatalf("invalid node details accepted: %+v", invalid)
		}
	}
	current, err := client.Snapshot(t.Context())
	if err != nil || !current.Settings.Equal(snapshot.Settings) {
		t.Fatalf("rejected settings changed state: %+v, %v", current.Settings, err)
	}
	saved, err := (store.Disk{Dir: directory}).Load()
	if err != nil || saved.Settings.Details != snapshot.Settings.Details || saved.Settings.DNS != snapshot.Settings.DNS {
		t.Fatalf("persisted settings: %+v, %v", saved.Settings, err)
	}
	config, err := os.ReadFile(ipc.Config(directory))
	if err != nil || strings.Contains(string(config), `"autostart"`) {
		t.Fatalf("autostart stored in config: %s, %v", config, err)
	}
	shutdownDone := make(chan struct{})
	go func() { server.Shutdown(); close(shutdownDone) }()
	select {
	case <-shutdownDone:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not close Watch")
	}
	requireNoError(t, <-serveDone)
}

type refreshTransport func(*http.Request) (*http.Response, error)

func (transport refreshTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func sendRequest(ctx context.Context, server *Server, request ipc.Request) error {
	client, daemon := net.Pipe()
	defer func() { _ = client.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = client.Close() })
	defer stop()
	server.sem <- struct{}{}
	go server.handle(daemon)
	request.Version = version.Version
	if err := json.NewEncoder(client).Encode(request); err != nil {
		return err
	}
	var response ipc.Response
	err := json.NewDecoder(client).Decode(&response)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return err
	}
	if response.Error != nil {
		return response.Error
	}
	return nil
}

func TestSubscriptionRefresh(t *testing.T) {
	var body atomic.Value
	first, second := "trojan://secret@example.com:443#first", "trojan://secret@example.com:443#second"
	body.Store(first + "\n" + second)
	source := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, req *http.Request) {
		_, _ = io.WriteString(response, body.Load().(string))
	}))
	defer source.Close()

	directory := t.TempDir()
	disk := store.Disk{Dir: directory}
	app, err := core.New(t.Context(), disk, connection.New(t.Context(), directory, nil), subscription.New(t.Context()))
	requireNoError(t, err)
	listener, err := net.Listen("unix", filepath.Join(directory, "daemon.sock"))
	requireNoError(t, err)
	server := New(t.Context(), app)
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	defer func() {
		server.Shutdown()
		if err := <-serveDone; err != nil {
			t.Error(err)
		}
	}()

	client := ipc.New(listener.Addr().String())
	added, err := client.AddSubscription(t.Context(), source.URL)
	requireNoError(t, err)
	before, err := disk.Load()
	requireNoError(t, err)
	if nodes := before.Subscriptions[0].Nodes; len(nodes) != 2 || nodes[0].ID == nodes[1].ID {
		t.Fatalf("duplicate links were merged or given the same ID: %d nodes", len(nodes))
	}
	refresh := func(test *testing.T, content string) store.State {
		test.Helper()
		body.Store(content)
		requireNoError(test, client.RefreshSubscriptions(test.Context(), added.SubscriptionID))
		state, err := disk.Load()
		requireNoError(test, err)
		return state
	}
	invalidImports := map[string]string{
		"JSON empty array":     `[]`,
		"JSON empty object":    `{}`,
		"sing-box empty":       `{"outbounds":[],"endpoints":[]}`,
		"sing-box infra":       `{"outbounds":[{"type":"direct"},{"type":"block"},{"type":"dns"},{"type":"selector","outbounds":["direct"]},{"type":"urltest","outbounds":["direct"]}]}`,
		"xray infra":           `[{"remarks":"empty","outbounds":[{"protocol":"freedom"},{"protocol":"blackhole"},{"protocol":"dns"},{"protocol":"loopback"}]}]`,
		"clash malformed":      "proxies:\n  - {name: bad, type: trojan, server: example.com, port: [}\n",
		"reserved negative":    "proxies:\n  - {type: wireguard, server: example.com, port: 51820, private-key: secret, public-key: public, ip: 10.0.0.2, reserved: [-1, 0, 255]}",
		"reserved overflow":    "proxies:\n  - {type: wireguard, server: example.com, port: 51820, private-key: secret, public-key: public, ip: 10.0.0.2, reserved: [0, 256, 255]}",
		"reserved short":       "proxies:\n  - {type: wireguard, server: example.com, port: 51820, private-key: secret, public-key: public, ip: 10.0.0.2, reserved: [0, 255]}",
		"reserved long":        "proxies:\n  - {type: wireguard, server: example.com, port: 51820, private-key: secret, public-key: public, ip: 10.0.0.2, reserved: [0, 1, 2, 255]}",
		"sing-box transport":   `{"outbounds":[{"type":"shadowsocks","server":"example.com","server_port":443,"method":"aes-128-gcm","password":"secret","transport":{"type":"ws"}}]}`,
		"sing-box no UUID":     `{"outbounds":[{"type":"vless","server":"example.com","server_port":443}]}`,
		"sing-box no password": `{"outbounds":[{"type":"trojan","server":"example.com","server_port":443}]}`,
		"shadowtls detour":     `{"outbounds":[{"type":"shadowtls","tag":"stls","server":"example.com","server_port":443,"detour":"missing"},{"type":"shadowsocks","server":"example.com","server_port":8388,"method":"aes-128-gcm","password":"secret","detour":"stls"}]}`,
		"xray incompatible":    `{"outbounds":[{"protocol":"vless","settings":{"vnext":[{"address":"example.com","port":443,"users":[{"id":"uuid"}]}],"servers":[{"address":"ignored.example","port":443,"password":"secret"}]}}]}`,
		"truncated json":       `{"outbounds":[{"type":"vless","server":"example.com"`,
		"xhttp download":       `{"outbounds":[{"protocol":"trojan","settings":{"servers":[{"address":"example.com","port":443,"password":"secret"}]},"streamSettings":{"network":"xhttp","xhttpSettings":{"extra":{"downloadSettings":{"address":"download.example"}}}}}]}`,
		"xhttp clash download": "proxies:\n  - {type: trojan, server: example.com, port: 443, password: secret, network: xhttp, xhttp-opts: {download-settings: {server: download.example}}}",
		"xhttp mode":           `{"outbounds":[{"type":"vless","server":"example.com","server_port":443,"uuid":"uuid","transport":{"type":"xhttp","mode":"invalid"}}]}`,
		"xhttp padding":        `{"outbounds":[{"type":"vless","server":"example.com","server_port":443,"uuid":"uuid","transport":{"type":"xhttp","x_padding_bytes":"-1"}}]}`,
		"xhttp placement":      `{"outbounds":[{"type":"vless","server":"example.com","server_port":443,"uuid":"uuid","transport":{"type":"xhttp","session_placement":"body"}}]}`,
		"xhttp xmux":           `{"outbounds":[{"type":"vless","server":"example.com","server_port":443,"uuid":"uuid","transport":{"type":"xhttp","xmux":{"max_connections":2,"max_concurrency":2}}}]}`,
		"xhttp range":          `{"outbounds":[{"type":"vless","server":"example.com","server_port":443,"uuid":"uuid","transport":{"type":"xhttp","sc_max_each_post_bytes":1.5}}]}`,
		"hy2 negative up":      `{"outbounds":[{"type":"hysteria2","server":"example.com","server_port":443,"password":"secret","up_mbps":-1}]}`,
		"hy2 negative down":    `{"outbounds":[{"type":"hysteria2","server":"example.com","server_port":443,"password":"secret","down_mbps":-1}]}`,
		"hy2 clash up":         "proxies:\n  - {type: hysteria2, server: example.com, port: 443, password: secret, up: -1}",
		"hy2 clash down":       "proxies:\n  - {type: hysteria2, server: example.com, port: 443, password: secret, down: -1}",
		"hy1 invalid up":       "hysteria://secret@example.com:443?upmbps=secret",
		"hy1 invalid down":     "hysteria://secret@example.com:443?downmbps=secret",
		"hy1 negative up":      "hysteria://secret@example.com:443?upmbps=-1",
		"clash invalid rate":   "proxies:\n  - {type: hysteria2, server: example.com, port: 443, password: secret, up: 'secret Mbps'}",
		"shadowtls version":    "shadowtls://secret@example.com:443?version=secret",
		"URI invalid port":     "trojan://secret@example.com:secret",
		"hy2 up overflow":      fmt.Sprintf(`{"outbounds":[{"type":"hysteria2","server":"example.com","server_port":443,"password":"secret","up_mbps":%d}]}`, math.MaxInt/125000+1),
		"hy2 down overflow":    fmt.Sprintf(`{"outbounds":[{"type":"hysteria2","server":"example.com","server_port":443,"password":"secret","down_mbps":%d}]}`, math.MaxInt/125000+1),
		"awg negative junk":    `{"endpoints":[{"type":"wireguard","private_key":"secret","address":["10.0.0.2/32"],"peers":[{"address":"example.com","port":51820,"public_key":"peer"}],"jc":-1}]}`,
		"awg junk overflow":    `{"endpoints":[{"type":"wireguard","private_key":"secret","address":["10.0.0.2/32"],"peers":[{"address":"example.com","port":51820,"public_key":"peer"}],"jc":1,"jmin":4294967295,"jmax":4294967295}]}`,
		"awg junk allocation":  `{"endpoints":[{"type":"wireguard","private_key":"secret","address":["10.0.0.2/32"],"peers":[{"address":"example.com","port":51820,"public_key":"peer"}],"jc":65535,"jmin":65507,"jmax":65507}]}`,
		"awg CPS allocation":   `{"endpoints":[{"type":"wireguard","private_key":"secret","address":["10.0.0.2/32"],"peers":[{"address":"example.com","port":51820,"public_key":"peer"}],"i1":"<r 65507><t>"}]}`,
		"awg reversed header":  `{"endpoints":[{"type":"wireguard","private_key":"secret","address":["10.0.0.2/32"],"peers":[{"address":"example.com","port":51820,"public_key":"peer"}],"h1":"20-10"}]}`,
		"awg IPC injection":    `{"endpoints":[{"type":"wireguard","private_key":"secret","address":["10.0.0.2/32"],"peers":[{"address":"example.com","port":51820,"public_key":"peer"}],"i1":"<b 0xff>\nreplace_peers=true"}]}`,
		"awg clash keepalive":  "proxies:\n  - {type: wireguard, server: example.com, port: 51820, private-key: secret, public-key: peer, ip: 10.0.0.2, persistent-keepalive: '20-10'}",
		"awg clash junk":       "proxies:\n  - {type: wireguard, server: example.com, port: 51820, private-key: secret, public-key: peer, ip: 10.0.0.2, amnezia-wg-option: {jc: 'null'}}",
		"awg clash legacy":     "proxies:\n  - {type: wireguard, server: example.com, port: 51820, private-key: secret, public-key: peer, ip: 10.0.0.2, amnezia-wg-option: {j1: '<b 0xff>'}}",
		"awg URI keepalive":    "wireguard://secret@example.com:51820?publickey=peer&address=10.0.0.2&keepalive=-1",
		"awg URI injection":    "wireguard://secret@example.com:51820?publickey=peer&address=10.0.0.2&I1=%3Cb%200xff%3E%0Areplace_peers=true",
		"awg URI aliases":      "wireguard://secret@example.com:51820?publickey=peer&address=10.0.0.2&h1=100&awg_h1=200",
		"awg URI legacy":       "wireguard://secret@example.com:51820?publickey=peer&address=10.0.0.2&J1=%3Cb%200xff%3E",
		"awg URI unknown":      "wireguard://secret@example.com:51820?publickey=peer&address=10.0.0.2&awg_unknown=1",
		"vpn base64":           "vpn://!",
		"vpn compressed":       "vpn://" + base64.RawURLEncoding.EncodeToString([]byte("0000invalid-zlib")),
	}
	const amneziaClient = `{"client_priv_key":"secret","server_pub_key":"peer","client_ip":"10.0.0.2"}`
	container := fmt.Sprintf(`{"container":"amnezia-awg2","awg":{"port":"51820","last_config":%q}}`, amneziaClient)
	for name, profile := range map[string]string{
		"vpn malformed JSON":   `{"containers":[`,
		"vpn future version":   `{"format_version":2}`,
		"vpn admin export":     `{"defaultContainer":"amnezia-awg2","containers":[{"container":"amnezia-awg2","awg":{}}]}`,
		"vpn wrong default":    `{"defaultContainer":"amnezia-openvpn","containers":[` + container + `]}`,
		"vpn ambiguous":        `{"hostName":"example.com","containers":[` + container + `,` + container + `]}`,
		"vpn missing keys":     `{"hostName":"example.com","containers":[{"container":"amnezia-awg2","awg":{"port":"51820","last_config":"{}"}}]}`,
		"vpn ignores SSH port": fmt.Sprintf(`{"hostName":"example.com","port":22,"containers":[{"container":"amnezia-awg2","awg":{"last_config":%q}}]}`, amneziaClient),
		"vpn expansion limit":  `{"description":"` + strings.Repeat("x", 1<<20) + `"}`,
	} {
		encoded := bytes.NewBuffer(binary.BigEndian.AppendUint32(nil, uint32(len(profile))))
		compressor := zlib.NewWriter(encoded)
		_, err := compressor.Write([]byte(profile))
		requireNoError(t, err)
		requireNoError(t, compressor.Close())
		invalidImports[name] = "vpn://" + base64.RawURLEncoding.EncodeToString(encoded.Bytes())
	}
	for name, content := range invalidImports {
		t.Run(name, func(t *testing.T) {
			body.Store(content)
			if err := client.RefreshSubscriptions(t.Context(), added.SubscriptionID); err == nil || strings.Contains(err.Error(), "secret") {
				t.Fatalf("refresh error: %v", err)
			}
			after, err := disk.Load()
			if err != nil || !reflect.DeepEqual(after.Subscriptions, before.Subscriptions) {
				t.Fatalf("failed refresh changed subscriptions: before=%+v after=%+v err=%v", before.Subscriptions, after.Subscriptions, err)
			}
		})
	}
	after := refresh(t, second+"\n"+first)
	old, current := before.Subscriptions[0].Nodes, after.Subscriptions[0].Nodes
	if len(current) != 2 || current[0].ID != old[1].ID || current[1].ID != old[0].ID {
		t.Fatal("refresh did not preserve separate IDs across reordering")
	}
	shadowsocks := "ss://" + base64.StdEncoding.EncodeToString([]byte("aes-256-gcm:pa%2Fss@word@example.com:8388")) + "#ss"
	partial := refresh(t, base64.StdEncoding.EncodeToString([]byte(first+"\n"+shadowsocks+"\nv2rayn://hysteria2/opaque\nvless://uuid@example.com:443?type=quic")))
	if nodes := partial.Subscriptions[0].Nodes; len(nodes) != 2 || nodes[0].Name != "first" || nodes[1].Protocol != domain.SS || nodes[1].Server != "example.com" || nodes[1].Auth.Password != "pa%2Fss@word" {
		t.Fatalf("mixed refresh did not save the supported node: %+v", nodes)
	}
	if warning := partial.Subscriptions[0].Warning; warning != `skipped nodes: 2 (unsupported transport: quic: 1; unsupported: 1)` {
		t.Fatalf("partial refresh did not persist a useful warning: %q", warning)
	}
	if snapshot, err := client.Snapshot(t.Context()); err != nil || snapshot.Subscriptions[0].Warning != partial.Subscriptions[0].Warning {
		t.Fatalf("partial refresh warning is missing from IPC: %+v, %v", snapshot.Subscriptions, err)
	}
	body.Store("vless://uuid@example.com:443?type=quic")
	if err := client.RefreshSubscriptions(t.Context(), added.SubscriptionID); err == nil || !strings.Contains(err.Error(), `skipped nodes: 1 (unsupported transport: quic: 1)`) {
		t.Fatalf("unusable-only refresh: %v", err)
	}
	unchanged, err := disk.Load()
	if err != nil || !reflect.DeepEqual(unchanged.Subscriptions, partial.Subscriptions) {
		t.Fatalf("unusable-only refresh changed subscriptions: %v", err)
	}
	if clean := refresh(t, first); clean.Subscriptions[0].Warning != "" {
		t.Fatalf("clean refresh kept the old warning: %+v", clean.Subscriptions)
	}
	for _, fixture := range []struct {
		name, content string
		nodes         []domain.Node
		skipped       int
	}{
		{
			name: "sing-box selector between detours",
			content: `[
				{"type":"shadowsocks","tag":"ss","server":"ignored.example","server_port":8388,"method":"aes-128-gcm","password":"secret","detour":"stls"},
				{"type":"selector","tag":"select","outbounds":["ss"]},
				{"type":"urltest","tag":"auto","outbounds":["ss"]},
				{"type":"shadowtls","tag":"stls","version":3,"server":"cover.example","server_port":443,"password":"shadow","tls":{"enabled":true,"server_name":"sni.example"}}
			]`,
			nodes: []domain.Node{{Name: "ss", Protocol: domain.SS, Server: "cover.example", Port: 443, Auth: domain.Auth{Method: "aes-128-gcm", Password: "secret"}, ShadowTLS: &domain.ShadowTLS{Version: 3, Password: "shadow", SNI: "sni.example"}}},
		},
		{
			name: "sing-box document boundaries",
			content: `[{
				"outbounds":[
					{"type":"shadowsocks","tag":"first-ss","server":"ignored.example","server_port":8388,"method":"aes-128-gcm","password":"first-auth","detour":"stls"},
					{"type":"shadowtls","tag":"stls","version":3,"server":"first.example","server_port":443,"password":"first-shadow","tls":{"enabled":true,"server_name":"first-cover.example"}}
				]
			},{
				"outbounds":[
					{"type":"shadowtls","tag":"stls","version":3,"server":"second.example","server_port":8443,"password":"second-shadow","tls":{"enabled":true,"server_name":"second-cover.example"}},
					{"type":"shadowsocks","tag":"second-ss","server":"ignored.example","server_port":8388,"method":"aes-256-gcm","password":"second-auth","detour":"stls"}
				]
			}]`,
			nodes: []domain.Node{
				{Name: "first-ss", Protocol: domain.SS, Server: "first.example", Port: 443, Auth: domain.Auth{Method: "aes-128-gcm", Password: "first-auth"}, ShadowTLS: &domain.ShadowTLS{Version: 3, Password: "first-shadow", SNI: "first-cover.example"}},
				{Name: "second-ss", Protocol: domain.SS, Server: "second.example", Port: 8443, Auth: domain.Auth{Method: "aes-256-gcm", Password: "second-auth"}, ShadowTLS: &domain.ShadowTLS{Version: 3, Password: "second-shadow", SNI: "second-cover.example"}},
			},
		},
		{
			name: "xray document boundaries and endpoints",
			content: `[{
				"remarks":"First",
				"outbounds":[
					{"protocol":"vless","tag":"proxy","settings":{"vnext":[
						{"address":"first.example","port":443,"users":[{"id":"first"},{"id":"second"}]},
						{"address":"next.example","port":8443,"users":[{"id":"third"}]}
					]}},
					{"protocol":"trojan","tag":"fallback","settings":{"servers":[
						{"address":"fallback.example","port":443,"password":"first-password"},
						{"address":"backup.example","port":8443,"password":"second-password"}
					]}},
					{"protocol":"freedom"}
				]
			},{
				"remarks":"Second",
				"outbounds":[{"protocol":"vless","tag":"unique","settings":{"vnext":[{"address":"last.example","port":443,"users":[{"id":"last"}]}]}}]
			}]`,
			nodes: []domain.Node{
				{Name: "First", Protocol: domain.VLESS, Server: "first.example", Port: 443, Auth: domain.Auth{UUID: "first"}},
				{Name: "First", Protocol: domain.VLESS, Server: "first.example", Port: 443, Auth: domain.Auth{UUID: "second"}},
				{Name: "First", Protocol: domain.VLESS, Server: "next.example", Port: 8443, Auth: domain.Auth{UUID: "third"}},
				{Name: "First (fallback)", Protocol: domain.Trojan, Server: "fallback.example", Port: 443, Auth: domain.Auth{Password: "first-password"}},
				{Name: "First (fallback)", Protocol: domain.Trojan, Server: "backup.example", Port: 8443, Auth: domain.Auth{Password: "second-password"}},
				{Name: "Second", Protocol: domain.VLESS, Server: "last.example", Port: 443, Auth: domain.Auth{UUID: "last"}},
			},
		},
		{
			name:    "xray flat outbounds",
			content: `[{"protocol":"vless","tag":"first","settings":{"vnext":[{"address":"first.example","port":443,"users":[{"id":"first"}]}]}},{"protocol":"trojan","tag":"second","settings":{"servers":[{"address":"second.example","port":8443,"password":"second"}]}}]`,
			nodes: []domain.Node{
				{Name: "first", Protocol: domain.VLESS, Server: "first.example", Port: 443, Auth: domain.Auth{UUID: "first"}},
				{Name: "second", Protocol: domain.Trojan, Server: "second.example", Port: 8443, Auth: domain.Auth{Password: "second"}},
			},
		},
		{
			name:    "xray single outbound",
			content: `{"protocol":"trojan","tag":"single","settings":{"servers":[{"address":"single.example","port":443,"password":"single"}]}}`,
			nodes:   []domain.Node{{Name: "single", Protocol: domain.Trojan, Server: "single.example", Port: 443, Auth: domain.Auth{Password: "single"}}},
		},
		{
			name:    "sing-box single outbound",
			content: `{"type":"trojan","tag":"single","server":"single.example","server_port":443,"password":"single"}`,
			nodes:   []domain.Node{{Name: "single", Protocol: domain.Trojan, Server: "single.example", Port: 443, Auth: domain.Auth{Password: "single"}}},
		},
		{
			name:    "sing-box endpoint document",
			content: `{"endpoints":[{"type":"wireguard","tag":"endpoint","private_key":"private","address":["10.0.0.2/32"],"peers":[{"address":"endpoint.example","port":51820,"public_key":"public"}]}]}`,
			nodes:   []domain.Node{{Name: "endpoint", Protocol: domain.WG, Server: "endpoint.example", Port: 51820, WireGuard: &domain.WireGuard{PrivateKey: "private", PeerPublicKey: "public", Address: []string{"10.0.0.2/32"}}}},
		},
		{
			name: "clash reserved representations",
			content: "proxies:\n" +
				"  - {name: list, type: wireguard, server: example.com, port: 51820, private-key: private, public-key: public, ip: 10.0.0.2, reserved: [0, 128, 255]}\n" +
				"  - {name: base64, type: wireguard, server: example.com, port: 51820, private-key: private, public-key: public, ip: 10.0.0.2, reserved: AID/}\n",
			nodes: []domain.Node{
				{Name: "list", Protocol: domain.WG, Server: "example.com", Port: 51820, WireGuard: &domain.WireGuard{PrivateKey: "private", PeerPublicKey: "public", Address: []string{"10.0.0.2/32"}, Reserved: []uint8{0, 128, 255}}},
				{Name: "base64", Protocol: domain.WG, Server: "example.com", Port: 51820, WireGuard: &domain.WireGuard{PrivateKey: "private", PeerPublicKey: "public", Address: []string{"10.0.0.2/32"}, Reserved: []uint8{0, 128, 255}}},
			},
		},
		{
			name:    "sing-box mixed rejections",
			content: `[{"type":"trojan","tag":"valid","server":"example.com","server_port":443,"password":"secret"},{"type":"direct"},{"type":"future-proxy"},42,null,{}]`,
			nodes:   []domain.Node{{Name: "valid", Protocol: domain.Trojan, Server: "example.com", Port: 443, Auth: domain.Auth{Password: "secret"}}},
			skipped: 4,
		},
		{
			name:    "xray mixed rejections",
			content: `[{"protocol":"trojan","tag":"valid","settings":{"servers":[{"address":"example.com","port":443,"password":"secret"}]}},{"protocol":"freedom"},{"protocol":"future-proxy"},42,null,{}]`,
			nodes:   []domain.Node{{Name: "valid", Protocol: domain.Trojan, Server: "example.com", Port: 443, Auth: domain.Auth{Password: "secret"}}},
			skipped: 4,
		},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			after := refresh(t, fixture.content)
			nodes := after.Subscriptions[0].Nodes
			if len(nodes) != len(fixture.nodes) {
				t.Fatalf("imported %d nodes, want %d: %+v", len(nodes), len(fixture.nodes), nodes)
			}
			for index, expected := range fixture.nodes {
				node := nodes[index]
				foreignChanged := len(node.Native) == 0 && (node.Auth != expected.Auth || !reflect.DeepEqual(node.ShadowTLS, expected.ShadowTLS))
				if node.Name != expected.Name || node.Protocol != expected.Protocol || node.Server != expected.Server || node.Port != expected.Port || foreignChanged || !reflect.DeepEqual(node.WireGuard, expected.WireGuard) {
					t.Fatalf("node %d: got %+v, want %+v", index, node, expected)
				}
			}
			warning := after.Subscriptions[0].Warning
			if fixture.skipped == 0 && warning != "" || fixture.skipped != 0 && (!strings.Contains(warning, fmt.Sprintf("skipped nodes: %d (", fixture.skipped)) || !strings.Contains(warning, "unsupported: 1")) || strings.Contains(warning, "secret") {
				t.Fatalf("unexpected warning: %q", warning)
			}
		})
	}
	for _, fixture := range []struct{ content, password, name string }{
		{"aes-256-gcm:pa#ss@example.com:8388", "pa#ss", "example.com"},
		{"aes-256-gcm:pa?ss@example.com:8388", "pa?ss", "example.com"},
		{"aes-256-gcm:pa@ss#word?more%2F@example.com:8388", "pa@ss#word?more%2F", "example.com"},
		{"aes-256-gcm:pa@ss#word?more%2F@example.com:8388#user@work", "pa@ss#word?more%2F", "user@work"},
		{"aes-256-gcm:pass@example.com:8388?unused=user@work#embedded", "pass", "embedded"},
		{"aes-256-gcm:p@example.com:8388#remark@new.example:443", "p", "remark@new.example:443"},
	} {
		t.Run("legacy shadowsocks/"+fixture.password+"/"+fixture.name, func(t *testing.T) {
			after := refresh(t, "ss://"+base64.StdEncoding.EncodeToString([]byte(fixture.content)))
			if nodes := after.Subscriptions[0].Nodes; len(nodes) != 1 || nodes[0].Protocol != domain.SS || nodes[0].Server != "example.com" || nodes[0].Port != 8388 || nodes[0].Auth.Password != fixture.password || nodes[0].Name != fixture.name {
				t.Fatalf("legacy shadowsocks credentials or metadata changed: %+v", nodes)
			}
		})
	}
	for query, hostname := range map[string]string{
		"":                                    "proxy.example",
		"host=cover.example":                  "cover.example",
		"sni=tls.example&host=cover.example":  "tls.example",
		"peer=tls.example&host=cover.example": "tls.example",
		"server_name=tls.example":             "tls.example",
		"serverName=tls.example":              "tls.example",
	} {
		t.Run("shadowtls hostname/"+query, func(t *testing.T) {
			after := refresh(t, "shadowtls://secret@proxy.example:443?"+query)
			if len(after.Subscriptions[0].Nodes) != 1 {
				t.Fatalf("shadowtls refresh: %+v", after.Subscriptions)
			}
			node := after.Subscriptions[0].Nodes[0]
			if node.TLS == nil || node.ShadowTLS == nil || node.TLS.SNI != hostname || node.ShadowTLS.SNI != hostname {
				t.Fatalf("shadowtls hostname %q: TLS=%+v, ShadowTLS=%+v", hostname, node.TLS, node.ShadowTLS)
			}
		})
	}
	for name, content := range map[string]string{
		"clash invalid":           "proxies:\n  - {name: valid, type: trojan, server: example.com, port: 443, password: secret}\n  - {name: bad, type: tuic, server: example.com, port: 443, password: secret}",
		"xray xhttp defaults":     `{"outbounds":[{"protocol":"trojan","settings":{"servers":[{"address":"example.com","port":443,"password":"secret"}]},"streamSettings":{"network":"xhttp","xhttpSettings":null}},{"protocol":"future-proxy"}]}`,
		"sing-box xhttp defaults": `{"outbounds":[{"type":"vless","server":"example.com","server_port":443,"uuid":"uuid","transport":{"type":"xhttp"}},{"type":"future-proxy"}]}`,
		"vmess xhttp defaults":    "vmess://" + base64.StdEncoding.EncodeToString([]byte(`{"add":"example.com","port":443,"id":"uuid","net":"xhttp","type":"none","extra":null}`)) + "\nfuture://unsupported",
		"clash fields":            "proxies:\n  - {type: trojan, server: example.com, port: 443, password: secret}\n  - {type: trojan, port: [secret]}",
		"clash no type":           "proxies:\n  - {type: trojan, server: example.com, port: 443, password: secret}\n  - {name: bad}",
		"sing-box fields":         `{"outbounds":[{"type":"trojan","server":"example.com","server_port":443,"password":"secret"},{"type":"trojan","server_port":"secret"}]}`,
		"sing-box scalar":         `{"outbounds":[{"type":"trojan","server":"example.com","server_port":443,"password":"secret"},42]}`,
		"sing-box array":          `[{"type":"trojan","server":"example.com","server_port":443,"password":"secret"},42]`,
		"xray scalar":             `{"outbounds":[{"protocol":"trojan","settings":{"servers":[{"address":"example.com","port":443,"password":"secret"}]}},42]}`,
		"sing-box null":           `{"outbounds":[{"type":"trojan","server":"example.com","server_port":443,"password":"secret"},null]}`,
		"sing-box no type":        `{"outbounds":[{"type":"trojan","server":"example.com","server_port":443,"password":"secret"},{}]}`,
		"xray fields":             `{"outbounds":[{"protocol":"trojan","settings":{"servers":[{"address":"example.com","port":443,"password":"secret"}]}},{"protocol":"trojan","settings":{"servers":[{"port":"secret"}]}}]}`,
		"xray null":               `{"outbounds":[{"protocol":"trojan","settings":{"servers":[{"address":"example.com","port":443,"password":"secret"}]}},null]}`,
		"xray no protocol":        `{"outbounds":[{"protocol":"trojan","settings":{"servers":[{"address":"example.com","port":443,"password":"secret"}]}},{}]}`,
		"sing-box unknown":        `{"outbounds":[{"type":"trojan","server":"example.com","server_port":443,"password":"secret"},{"type":"future-proxy","tag":"unknown"}]}`,
		"sing-box hy2 zero":       `{"outbounds":[{"type":"hysteria2","server":"example.com","server_port":443,"password":"secret","up_mbps":0,"down_mbps":0},{"type":"future-proxy"}]}`,
		"clash hy2 bandwidth":     "proxies:\n  - {type: hysteria2, server: example.com, port: 443, password: secret, up: '50 Mbps', down: 200}\n  - {type: future-proxy}",
		"clash JSON":              `{"proxies":[{"type":"trojan","server":"example.com","port":443,"password":"secret"},{"type":"future-proxy"}]}`,
		"clash flow YAML":         "{proxies: [{type: trojan, server: example.com, port: 443, password: secret}, {type: future-proxy}]}",
		"base64 JSON":             base64.StdEncoding.EncodeToString([]byte(`{"outbounds":[{"type":"trojan","server":"example.com","server_port":443,"password":"secret"},{"type":"future-proxy"}]}`)),
		"sing-box hy2 maximum":    fmt.Sprintf(`{"outbounds":[{"type":"hysteria2","server":"example.com","server_port":443,"password":"secret","up_mbps":%d,"down_mbps":%d},{"type":"future-proxy"}]}`, math.MaxInt/125000, math.MaxInt/125000),
		"xray unknown":            `{"outbounds":[{"protocol":"trojan","settings":{"servers":[{"address":"example.com","port":443,"password":"secret"}]}},{"protocol":"future-proxy","tag":"unknown"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			after := refresh(t, content)
			if len(after.Subscriptions[0].Nodes) != 1 || !strings.Contains(after.Subscriptions[0].Warning, "skipped nodes: 1 (") || strings.Contains(after.Subscriptions[0].Warning, "secret") {
				t.Fatalf("mixed refresh did not save the node and warning: %+v", after.Subscriptions)
			}
			if name == "sing-box hy2 zero" || name == "sing-box hy2 maximum" {
				expected := 0
				if name == "sing-box hy2 maximum" {
					expected = math.MaxInt / 125000
				}
				if node := after.Subscriptions[0].Nodes[0]; node.Protocol != domain.HY2 || node.UpMbps != expected || node.DownMbps != expected {
					t.Fatalf("HY2 refresh changed bandwidth: %+v", node)
				}
			}
			if node := after.Subscriptions[0].Nodes[0]; name == "clash hy2 bandwidth" && (node.Protocol != domain.HY2 || node.UpMbps != 50 || node.DownMbps != 200) {
				t.Fatalf("Clash HY2 refresh changed bandwidth: %+v", node)
			}
			if strings.HasPrefix(name, "sing-box") {
				body.Store(`{"outbounds":[{"type":"future-proxy"}]}`)
			} else if strings.HasPrefix(name, "clash") {
				body.Store("proxies:\n  - {type: trojan, server: example.com, port: 443}")
			} else {
				body.Store(`{"outbounds":[{"protocol":"future-proxy"}]}`)
			}
			if err := client.RefreshSubscriptions(t.Context(), added.SubscriptionID); err == nil {
				t.Fatal("unsupported-only refresh succeeded")
			}
			unchanged, err := disk.Load()
			if err != nil || !reflect.DeepEqual(unchanged.Subscriptions, after.Subscriptions) {
				t.Fatalf("unsupported-only refresh changed saved nodes: %v", err)
			}
		})
	}
	synctest.Test(t, func(t *testing.T) {
		started, release := make(chan context.Context, 1), make(chan struct{})
		previousTransport := http.DefaultTransport
		defer func() { http.DefaultTransport = previousTransport }()
		http.DefaultTransport = refreshTransport(func(request *http.Request) (*http.Response, error) {
			started <- request.Context()
			select {
			case <-release:
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(first))}, nil
			case <-request.Context().Done():
				return nil, request.Context().Err()
			}
		})
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		defer cancel()
		ownerCtx, cancelOwner := context.WithCancel(ctx)
		defer cancelOwner()
		request := ipc.Request{Method: "RefreshSubscriptions", Arguments: ipc.Arguments{SubscriptionID: added.SubscriptionID}}
		done := make(chan error, 1)
		go func() {
			done <- sendRequest(ownerCtx, server, request)
		}()
		providerCtx := <-started
		followerDone := make(chan error, 1)
		go func() {
			followerDone <- sendRequest(ctx, server, request)
		}()
		synctest.Wait()
		if err := sendRequest(ctx, server, ipc.Request{Method: "SetCollapsed", Arguments: ipc.Arguments{SubscriptionID: added.SubscriptionID, Collapsed: new(true)}}); err != nil {
			t.Fatalf("mutation blocked on refresh: %v", err)
		}
		cancelOwner()
		synctest.Wait()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled refresh: %v", err)
		}
		if providerCtx.Err() != nil {
			t.Fatal("canceled owner stopped the shared HTTP request")
		}
		close(release)
		requireNoError(t, <-followerDone)
	})
	snap, err := client.Snapshot(t.Context())
	if err != nil || len(snap.Nodes) != 1 || snap.Nodes[0].Name != "first" || snap.Subscriptions[0].Refreshing ||
		len(snap.Collapsed) != 1 || snap.Collapsed[0] != added.SubscriptionID {
		t.Fatalf("refresh lost a concurrent change: %+v, %v", snap, err)
	}
	body.Store(first)
	_, err = client.AddSubscription(t.Context(), source.URL)
	requireNoError(t, err)
	body.Store(second)
	requireNoError(t, client.RefreshSubscriptions(t.Context(), added.SubscriptionID))
	snapshot, err := client.Snapshot(t.Context())
	if err != nil || len(snapshot.Nodes) != 2 || snapshot.Nodes[0].Name != "second" || snapshot.Nodes[1].Name != "first" {
		t.Fatalf("selected refresh changed the wrong subscriptions: %+v, %v", snapshot.Nodes, err)
	}
	requireNoError(t, client.RefreshSubscriptions(t.Context(), ""))
	snapshot, err = client.Snapshot(t.Context())
	if err != nil || len(snapshot.Nodes) != 2 || snapshot.Nodes[0].Name != "second" || snapshot.Nodes[1].Name != "second" {
		t.Fatalf("refresh all missed a subscription: %+v, %v", snapshot.Nodes, err)
	}
	t.Run("large escaped URL", func(t *testing.T) {
		rawURL := source.URL + "/feed#"
		rawURL += strings.Repeat("<", (4<<20)-len(rawURL))
		imported, err := client.AddSubscription(t.Context(), rawURL)
		requireNoError(t, err)
		state, err := disk.Load()
		requireNoError(t, err)
		last := state.Subscriptions[len(state.Subscriptions)-1]
		if last.ID != imported.SubscriptionID || last.URL != rawURL || len(last.Nodes) != 1 || last.Nodes[0].Name != "second" {
			t.Fatalf("large escaped URL import: matching ID=%v, matching URL=%v, nodes=%d, name=%q", last.ID == imported.SubscriptionID, last.URL == rawURL, len(last.Nodes), last.Name)
		}
	})
}

func TestSwitch(t *testing.T) {
	directory := t.TempDir()
	disk := store.Disk{Dir: directory}
	if err := disk.SaveState(store.State{Subscriptions: []store.Subscription{{ID: "sub", Nodes: []domain.Node{
		{ID: "a", Name: "A", Server: "a.example"}, {ID: "b", Name: "B", Server: "b.example"},
	}}}}); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(directory, "daemon.sock")
	client := ipc.New(socket)
	a := domain.NodeRef{SubscriptionID: "sub", NodeID: "a"}
	b := domain.NodeRef{SubscriptionID: "sub", NodeID: "b"}
	tun := true
	start := func(failB, failTun error) (*core.Core, func()) {
		app, err := core.New(t.Context(), disk, connection.New(t.Context(), directory, func(context.Context, string) engine.Engine {
			return &switchEngine{failB: failB, failTun: failTun}
		}), subscription.New(t.Context()))
		requireNoError(t, err)
		if err := app.Restore(); err != nil {
			if !errors.Is(err, ipc.ErrElevate) {
				t.Fatal(err)
			}
			app.RestoreFailed(err)
		}
		select {
		case <-app.RestartRequested():
			t.Fatal("restoring state requested an IPC restart")
		default:
		}
		listener, err := net.Listen("unix", socket)
		requireNoError(t, err)
		server := New(t.Context(), app)
		done := make(chan error, 1)
		go func() { done <- server.Serve(listener); close(done) }()
		stop := func() {
			server.Shutdown()
			if err, ok := <-done; ok && err != nil {
				t.Error(err)
			}
			app.Shutdown()
		}
		t.Cleanup(stop)
		return app, stop
	}
	check := func(ref domain.NodeRef, mode, pending bool) {
		state, err := disk.Load()
		requireNoError(t, err)
		snapshot, err := client.Snapshot(t.Context())
		requireNoError(t, err)
		if state.Active != ref || state.Last != ref || state.Tun != mode || (state.Pending != nil) != pending ||
			snapshot.Selected != ref || !pending && (!snapshot.Status.Connected || snapshot.Status.NodeRef != ref || snapshot.Status.Tun != mode) {
			t.Fatalf("state=%+v snapshot=%+v", state, snapshot)
		}
	}
	failure := errors.New("elevation required")
	_, stop := start(failure, failure)
	requireNoError(t, client.Connect(t.Context(), a, nil))
	if err := client.Connect(t.Context(), b, &tun); err == nil || errors.Is(err, ipc.ErrElevate) {
		t.Fatalf("ordinary switch failure was treated as elevation: %v", err)
	}
	check(a, false, false)
	if err := client.SetTun(t.Context(), true); err == nil || errors.Is(err, ipc.ErrElevate) {
		t.Fatalf("ordinary TUN failure was treated as elevation: %v", err)
	}
	check(a, false, false)
	stop()

	permission := errors.Join(os.ErrPermission, errors.New("operation not permitted"))
	if !elevate.Needed(permission) {
		return
	}
	app, stop := start(permission, permission)
	check(a, false, false)
	if err := client.Connect(t.Context(), b, &tun); !errors.Is(err, ipc.ErrElevate) {
		t.Fatalf("elevation request: %v", err)
	}
	check(a, false, true)
	state, err := disk.Load()
	if err != nil || state.Pending.Ref != b || !state.Pending.Tun {
		t.Fatalf("pending B: %+v, %v", state, err)
	}
	select {
	case <-app.RestartRequested():
	case <-time.After(time.Second):
		t.Fatal("daemon did not request elevation")
	}
	stop()

	_, stop = start(permission, permission)
	check(a, false, false)
	if err := client.SetTun(t.Context(), true); !errors.Is(err, ipc.ErrElevate) {
		t.Fatalf("mode elevation request: %v", err)
	}
	check(a, false, true)
	state, err = disk.Load()
	if err != nil || state.Pending.Ref != a || !state.Pending.Tun {
		t.Fatalf("pending TUN: %+v, %v", state, err)
	}
	stop()

	_, stop = start(permission, permission)
	check(a, false, false)
	if err := client.Connect(t.Context(), b, &tun); err == nil || err.Error() != ipc.ErrElevate.Error() {
		t.Fatalf("elevation request: %v", err)
	}
	stop()

	_, _ = start(nil, nil)
	check(b, true, false)
}
