package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
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
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/luynrs/justray/internal/client/tui"
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
	listener, err := net.Listen("unix", filepath.Join(directory, "daemon.sock"))
	requireNoError(t, err)
	logger := log.New(io.Discard, "", 0)
	app, err := core.New(t.Context(), store.Disk{Dir: directory}, connection.New(t.Context(), directory, nil, logger), subscription.New(t.Context(), logger))
	requireNoError(t, err)
	server := New(t.Context(), logger, app)
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
	if err := readSnapshot(); err != nil || snapshot.Settings.Port != domain.DefaultPort {
		t.Fatalf("initial snapshot: %+v, %v", snapshot, err)
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
	if err := readSnapshot(); err != nil || len(snapshot.Nodes) != 1 || snapshot.Nodes[0].SubscriptionID != added.SubscriptionID {
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
	settings.ForceTTY = "on"
	requireNoError(t, client.SetSettings(t.Context(), snapshot.Settings, settings))
	config, err := os.ReadFile(ipc.Config(directory))
	if err != nil || strings.Contains(string(config), `"autostart"`) {
		t.Fatalf("autostart stored in config: %s, %v", config, err)
	}
	t.Run("compact navigation", func(t *testing.T) {
		frames := make(chan string, 1)
		program := tea.NewProgram(tui.New(client, nil), tea.WithInput(nil), tea.WithOutput(io.Discard),
			tea.WithWindowSize(80, 10), tea.WithoutSignalHandler(),
			tea.WithFilter(func(m tea.Model, msg tea.Msg) tea.Msg {
				if _, ok := msg.(chan string); ok {
					frames <- ansi.Strip(m.View().Content)
					return nil
				}
				return msg
			}))
		done := make(chan error, 1)
		go func() { _, err := program.Run(); done <- err }()
		defer func() {
			program.Send(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
			select {
			case err := <-done:
				if err != nil {
					t.Error(err)
				}
			case <-time.After(3 * time.Second):
				program.Kill()
				t.Error("TUI did not stop")
			}
		}()
		screen := func(keys string, messages ...tea.Msg) string {
			for _, message := range messages {
				program.Send(message)
			}
			for _, key := range keys {
				msg := tea.KeyPressMsg{Code: key}
				if unicode.IsPrint(key) {
					msg.Text = string(key)
				}
				program.Send(msg)
			}
			program.Send(frames)
			select {
			case frame := <-frames:
				return frame
			case <-time.After(3 * time.Second):
				t.Fatal("TUI did not render")
				return ""
			}
		}
		check := func(test *testing.T, frame string, texts ...string) {
			test.Helper()
			for _, text := range texts {
				if !strings.Contains(frame, text) {
					test.Fatalf("missing %q:\n%s", text, frame)
				}
			}
		}
		scroll := func(message tea.Msg) string {
			for range 20 {
				program.Send(message)
			}
			return screen("")
		}
		waitFor := func(test *testing.T, keys, text string) string {
			test.Helper()
			for deadline := time.Now().Add(3 * time.Second); ; {
				frame := screen(keys)
				if strings.Contains(frame, text) {
					return frame
				}
				if time.Now().After(deadline) {
					test.Fatalf("timed out waiting for %q:\n%s", text, frame)
				}
				time.Sleep(time.Millisecond)
			}
		}
		waitFor(t, "o", "General")
		screen("q")
		for _, pane := range []string{"tree", "settings"} {
			program.Send(tea.WindowSizeMsg{Width: 80, Height: 10})
			if pane == "settings" {
				screen("o\t\t")
			}
			if counted, repeated := screen("gg2j"), screen("ggjj"); counted != repeated {
				t.Fatalf("%s count differs from repeated movement", pane)
			}
			if first, returned := screen("gg"), screen("2j2k"); first != returned {
				t.Fatalf("%s counted movement did not return to the first row", pane)
			}
			for _, keys := range [][2]string{{"2G", "ggj"}, {"3gg", "ggjj"}, {"999999999999999999999999G", "G"}} {
				if counted, expected := screen(keys[0]), screen(keys[1]); counted != expected {
					t.Fatalf("%s %q differs from %q", pane, keys[0], keys[1])
				}
			}
			for _, page := range []struct {
				start string
				key   rune
				alias rune
			}{{"gg", tea.KeyPgDown, 'f'}, {"G", tea.KeyPgUp, 'b'}} {
				before := screen(page.start)
				program.Send(tea.KeyPressMsg{Code: page.key})
				after := screen("")
				screen(page.start)
				program.Send(tea.KeyPressMsg{Code: page.alias, Mod: tea.ModCtrl})
				if after == before || screen("") != after {
					t.Fatalf("%s page movement or its Ctrl alias did not work", pane)
				}
			}
			screen("?")
			if counted, single := screen("gg9j"), screen("ggj"); counted != single {
				t.Fatalf("%s help applied a numeric prefix", pane)
			}
			program.Send(tea.WindowSizeMsg{Width: 80, Height: 12})
			frame := screen("gg")
			if !strings.Contains(frame, "Move") || !strings.Contains(frame, "Quit") {
				t.Fatalf("%s essential shortcuts require scrolling at 80x12:\n%s", pane, frame)
			}
			for _, width := range []int{24, 12, 1, 80} {
				program.Send(tea.WindowSizeMsg{Width: width, Height: 40})
				for _, line := range strings.Split(screen("gg"), "\n") {
					if ansi.StringWidth(line) > width {
						t.Fatalf("help exceeds terminal width %d: %q", width, line)
					}
				}
			}
			if frame := screen("\x1b"); strings.Contains(frame, "Shortcuts") {
				t.Fatal("Esc did not close help")
			}
		}
		screen("gg4j\r")
		program.Send(tea.PasteMsg{Content: "example.com"})
		if frame := screen("\r"); !strings.Contains(frame, "example.com") || !strings.Contains(frame, "Direct (1)") {
			t.Fatal("routing rule was not added to Direct")
		}
		if frame := screen("d"); strings.Contains(frame, "example.com") || !strings.Contains(frame, "Direct (0)") {
			t.Fatal("routing rule was not removed from Direct")
		}
		screen("q")
		controls := []struct {
			name      string
			down      tea.Msg
			up        tea.Msg
			firstLine int
		}{
			{"wheel", tea.MouseWheelMsg{Button: tea.MouseWheelDown, X: 10, Y: 3}, tea.MouseWheelMsg{Button: tea.MouseWheelUp, X: 10, Y: 3}, 2},
			{"keys", tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl}, tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl}, 4},
		}
		for _, control := range controls {
			t.Run("settings scroll focus/"+control.name, func(t *testing.T) {
				screen("ogg", tea.WindowSizeMsg{Width: 80, Height: 10})
				defer screen("q")
				check(t, screen("", tea.MouseClickMsg{Button: tea.MouseLeft, X: 5, Y: 6}), "| Auto-refresh")
				check(t, strings.Split(screen("", control.down), "\n")[control.firstLine], "| Auto-refresh")
				if frame := scroll(control.down); strings.Contains(frame, "| ") || !strings.Contains(frame, domain.DefaultProbeURL) {
					t.Fatalf("scroll did not reach the bottom with the selected setting offscreen:\n%s", frame)
				}
				check(t, scroll(control.up), "| Auto-refresh")
				scroll(control.down)
				check(t, screen("\r"), "| Auto-refresh")
				check(t, screen("\r", tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}, tea.PasteMsg{Content: "12h"}), "| Auto-refresh", "| 12h")
				screen("\t\tgg")
				check(t, scroll(control.down), "Block (0)")
				check(t, strings.Split(scroll(control.up), "\n")[2], "| Mode")
			})
		}
		if _, err := client.AddSubscription(t.Context(), link+"#Scroll%20anchor"); err != nil {
			t.Fatal(err)
		}
		feed := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Profile-Title", "Scroll feed")
			_, _ = io.WriteString(writer, strings.Join([]string{link + "#Remote-one", link + "#Remote-two", link + "#Remote-three", link + "#Remote-four"}, "\n"))
		}))
		defer feed.Close()
		if _, err := client.AddSubscription(t.Context(), feed.URL); err != nil {
			t.Fatal(err)
		}
		program.Send(tea.WindowSizeMsg{Width: 80, Height: 20})
		waitFor(t, "", "Remote-four")
		for _, control := range controls {
			t.Run("tree scroll focus/"+control.name, func(t *testing.T) {
				screen("gg", tea.WindowSizeMsg{Width: 80, Height: 10})
				check(t, screen("", tea.MouseClickMsg{Button: tea.MouseLeft, X: 5, Y: 5}), "|   o Scroll anchor")
				check(t, strings.Split(screen("", control.down), "\n")[control.firstLine], "|   o Scroll anchor")
				if frame := scroll(control.down); strings.Contains(frame, "Scroll anchor") || strings.Contains(frame, "| ") || !strings.Contains(frame, "Remote-four") {
					t.Fatalf("scroll did not reach the bottom with the selected node offscreen:\n%s", frame)
				}
				mode := "[ Proxy ]"
				if control.name == "keys" {
					mode = "[  TUN  ]"
				}
				requireNoError(t, client.SetTun(t.Context(), control.name == "keys"))
				if frame := waitFor(t, "", mode); strings.Contains(frame, "Scroll anchor") || !strings.Contains(frame, "Remote-four") {
					t.Fatalf("daemon update moved the viewport back to the selection:\n%s", frame)
				}
				if frame := screen("", tea.WindowSizeMsg{Width: 80, Height: 9}); strings.Contains(frame, "Scroll anchor") {
					t.Fatalf("resize moved the viewport back to the selection:\n%s", frame)
				}
				check(t, screen("d"), "Delete", "Scroll anchor")
				screen("\x1b")
				check(t, scroll(control.up), "|   o Scroll anchor", "v Default")
			})
		}
		screen("gga", tea.WindowSizeMsg{Width: 80, Height: 20})
		screen("\r", tea.PasteMsg{Content: "socks://[2001:db8::1]:19090#IPv6"})
		waitFor(t, "", "[2001:db8::1]:19090")
		addresses := []struct {
			name, server string
			port         int
			want         string
		}{
			{"raw IPv6", "2001:db8::1", 19090, "[2001:db8::1]:19090"},
			{"bracketed IPv6", "[2001:db8::2]", 19091, "[2001:db8::2]:19091"},
			{"raw zone", "fe80::1%eth0", 19092, "[fe80::1%eth0]:19092"},
			{"bracketed zone", "[fe80::2%eth0]", 19093, "[fe80::2%eth0]:19093"},
			{"mapped IPv6", "[::ffff:192.0.2.1]", 19094, "[::ffff:192.0.2.1]:19094"},
			{"IPv4", "192.0.2.1", 1, "192.0.2.1:1"},
			{"DNS", "proxy.example.test", 65535, "proxy.example.test:65535"},
		}
		for _, format := range []struct{ name, body, node, separator string }{
			{"JSON", `{"outbounds":[%s]}`, `{"tag":%q,"type":"socks","server":%q,"server_port":%d}`, ","},
			{"YAML", "proxies:\n%s", "  - {name: %q, type: socks5, server: %q, port: %d}", "\n"},
		} {
			t.Run("server addresses/"+format.name, func(t *testing.T) {
				nodes := make([]string, len(addresses))
				for i, address := range addresses {
					nodes[i] = fmt.Sprintf(format.node, format.name+" "+address.name, address.server, address.port)
				}
				feed := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
					writer.Header().Set("Profile-Title", format.name)
					_, _ = fmt.Fprintf(writer, format.body, strings.Join(nodes, format.separator))
				}))
				defer feed.Close()
				screen("gga", tea.WindowSizeMsg{Width: 100, Height: 40})
				screen("\r", tea.PasteMsg{Content: feed.URL})
				frame := waitFor(t, "", format.name+" DNS")
				for _, address := range addresses {
					_, line, found := strings.Cut(frame, format.name+" "+address.name)
					line, _, _ = strings.Cut(line, "\n")
					if !found || !strings.Contains(line, address.want) {
						t.Fatalf("%s %s: want %q:\n%s", format.name, address.name, address.want, frame)
					}
				}
			})
		}
	})

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
	logger := log.New(io.Discard, "", 0)
	app, err := core.New(t.Context(), disk, connection.New(t.Context(), directory, nil, logger), subscription.New(t.Context(), logger))
	requireNoError(t, err)
	listener, err := net.Listen("unix", filepath.Join(directory, "daemon.sock"))
	requireNoError(t, err)
	server := New(t.Context(), logger, app)
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
		requireNoError(test, client.RefreshSubscription(test.Context(), added.SubscriptionID))
		state, err := disk.Load()
		requireNoError(test, err)
		return state
	}
	for name, content := range map[string]string{
		"clash malformed":      "proxies:\n  - {name: bad, type: trojan, server: example.com, port: [}\n",
		"sing-box transport":   `{"outbounds":[{"type":"shadowsocks","server":"example.com","server_port":443,"method":"aes-128-gcm","password":"secret","transport":{"type":"ws"}}]}`,
		"shadowtls detour":     `{"outbounds":[{"type":"shadowtls","tag":"stls","server":"example.com","server_port":443},{"type":"shadowsocks","server":"example.com","server_port":8388,"method":"aes-128-gcm","password":"secret","detour":"stls"}]}`,
		"xray incompatible":    `{"outbounds":[{"protocol":"vless","settings":{"vnext":[{"address":"example.com","port":443,"users":[{"id":"uuid"}]}],"servers":[{"address":"ignored.example","port":443,"password":"secret"}]}}]}`,
		"truncated json":       `{"outbounds":[{"type":"vless","server":"example.com"`,
		"xhttp download":       `{"outbounds":[{"protocol":"trojan","settings":{"servers":[{"address":"example.com","port":443,"password":"secret"}]},"streamSettings":{"network":"xhttp","xhttpSettings":{"extra":{"downloadSettings":{"address":"download.example"}}}}}]}`,
		"xhttp clash download": "proxies:\n  - {type: trojan, server: example.com, port: 443, password: secret, network: xhttp, xhttp-opts: {download-settings: {server: download.example}}}",
		"xhttp mode":           `{"outbounds":[{"type":"vless","server":"example.com","server_port":443,"uuid":"uuid","transport":{"type":"xhttp","mode":"invalid"}}]}`,
		"xhttp padding":        `{"outbounds":[{"type":"vless","server":"example.com","server_port":443,"uuid":"uuid","transport":{"type":"xhttp","x_padding_bytes":"-1"}}]}`,
		"xhttp placement":      `{"outbounds":[{"type":"vless","server":"example.com","server_port":443,"uuid":"uuid","transport":{"type":"xhttp","session_placement":"body"}}]}`,
		"xhttp xmux":           `{"outbounds":[{"type":"vless","server":"example.com","server_port":443,"uuid":"uuid","transport":{"type":"xhttp","xmux":{"max_connections":2,"max_concurrency":2}}}]}`,
		"xhttp range":          `{"outbounds":[{"type":"vless","server":"example.com","server_port":443,"uuid":"uuid","transport":{"type":"xhttp","sc_max_each_post_bytes":1.5}}]}`,
		"xhttp path query":     `{"outbounds":[{"type":"vless","server":"example.com","server_port":443,"uuid":"uuid","transport":{"type":"xhttp","path":"/edge/?token=value"}}]}`,
		"hy2 negative up":      `{"outbounds":[{"type":"hysteria2","server":"example.com","server_port":443,"password":"secret","up_mbps":-1}]}`,
		"hy2 negative down":    `{"outbounds":[{"type":"hysteria2","server":"example.com","server_port":443,"password":"secret","down_mbps":-1}]}`,
		"hy2 clash up":         "proxies:\n  - {type: hysteria2, server: example.com, port: 443, password: secret, up: -1}",
		"hy2 clash down":       "proxies:\n  - {type: hysteria2, server: example.com, port: 443, password: secret, down: -1}",
		"hy2 up overflow":      fmt.Sprintf(`{"outbounds":[{"type":"hysteria2","server":"example.com","server_port":443,"password":"secret","up_mbps":%d}]}`, math.MaxInt/125000+1),
		"hy2 down overflow":    fmt.Sprintf(`{"outbounds":[{"type":"hysteria2","server":"example.com","server_port":443,"password":"secret","down_mbps":%d}]}`, math.MaxInt/125000+1),
	} {
		t.Run(name, func(t *testing.T) {
			body.Store(content)
			if err := client.RefreshSubscription(t.Context(), added.SubscriptionID); err == nil || strings.Contains(err.Error(), "secret") {
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
	if warning := partial.Subscriptions[0].Warning; warning != `skipped nodes: 2 (unsupported transport "quic": 1; unsupported: 1)` {
		t.Fatalf("partial refresh did not persist a useful warning: %q", warning)
	}
	if snapshot, err := client.Snapshot(t.Context()); err != nil || snapshot.Subscriptions[0].Warning != partial.Subscriptions[0].Warning {
		t.Fatalf("partial refresh warning is missing from IPC: %+v, %v", snapshot.Subscriptions, err)
	}
	body.Store("vless://uuid@example.com:443?type=quic")
	if err := client.RefreshSubscription(t.Context(), added.SubscriptionID); err == nil || !strings.Contains(err.Error(), `skipped nodes: 1 (unsupported transport "quic": 1)`) {
		t.Fatalf("unusable-only refresh: %v", err)
	}
	unchanged, err := disk.Load()
	if err != nil || !reflect.DeepEqual(unchanged.Subscriptions, partial.Subscriptions) {
		t.Fatalf("unusable-only refresh changed subscriptions: %v", err)
	}
	if clean := refresh(t, first); clean.Subscriptions[0].Warning != "" {
		t.Fatalf("clean refresh kept the old warning: %+v", clean.Subscriptions)
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
		"sing-box xhttp defaults": `{"outbounds":[{"type":"vless","server":"example.com","server_port":443,"uuid":"uuid","transport":{"type":"xhttp","extra":null}},{"type":"future-proxy"}]}`,
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
			if err := client.RefreshSubscription(t.Context(), added.SubscriptionID); err == nil {
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
		request := ipc.Request{Method: "RefreshSubscription", Arguments: ipc.Arguments{SubscriptionID: added.SubscriptionID}}
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
}

func TestSwitch(t *testing.T) {
	directory := t.TempDir()
	disk := store.Disk{Dir: directory}
	if err := disk.SaveState(store.State{Subscriptions: []store.Subscription{{ID: "sub", Nodes: []domain.Node{
		{ID: "a", Name: "A", Server: "a.example"}, {ID: "b", Name: "B", Server: "b.example"},
	}}}}); err != nil {
		t.Fatal(err)
	}
	logger := log.New(io.Discard, "", 0)
	socket := filepath.Join(directory, "daemon.sock")
	client := ipc.New(socket)
	a := domain.NodeRef{SubscriptionID: "sub", NodeID: "a"}
	b := domain.NodeRef{SubscriptionID: "sub", NodeID: "b"}
	tun := true
	start := func(failB, failTun error) (*core.Core, func()) {
		app, err := core.New(t.Context(), disk, connection.New(t.Context(), directory, func(context.Context, string) engine.Engine {
			return &switchEngine{failB: failB, failTun: failTun}
		}, logger), subscription.New(t.Context(), logger))
		requireNoError(t, err)
		if err := app.Restore(); err != nil {
			if !errors.Is(err, ipc.ErrElevate) {
				t.Fatal(err)
			}
			app.RestoreFailed(err)
		}
		listener, err := net.Listen("unix", socket)
		requireNoError(t, err)
		server := New(t.Context(), logger, app)
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
