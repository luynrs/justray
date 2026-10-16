package main

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	sbox "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter/certificate"
	"github.com/sagernet/sing-box/adapter/endpoint"
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/adapter/outbound"
	boxservice "github.com/sagernet/sing-box/adapter/service"
	"github.com/sagernet/sing-box/dns"
	"github.com/sagernet/sing-box/dns/transport/local"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/direct"
	"github.com/sagernet/sing-box/protocol/hysteria2"
	"github.com/sagernet/sing-box/protocol/wireguard"
	"github.com/sagernet/sing/common/json"

	"github.com/luynrs/justray/internal/daemon/store"
	"github.com/luynrs/justray/internal/ipc"
	"github.com/luynrs/justray/internal/platform/elevate"
)

func requireNoError(test *testing.T, err error) {
	test.Helper()
	if err != nil {
		test.Fatal(err)
	}
}

func TestStartup(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Setenv("TMPDIR", "/tmp")
	}
	home, err := os.MkdirTemp("", "jr-")
	requireNoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	bin := filepath.Join(home, "bin")
	build := exec.Command("go", "build", "-ldflags=-s -w", "-o", bin+string(os.PathSeparator), "./cmd/justray", "./cmd/justrayd")
	build.Dir = "../.."
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %s, %v", output, err)
	}
	for _, key := range []string{"HOME", "APPDATA", "XDG_CONFIG_HOME"} {
		t.Setenv(key, home)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	directory, err := ipc.Dir()
	requireNoError(t, err)
	requireNoError(t, ipc.EnsureDir(directory))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	requireNoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	requireNoError(t, os.WriteFile(ipc.Config(directory), fmt.Appendf(nil, `{"connection":{"port":%d}}`, port), 0o600))
	client := ipc.New(ipc.Socket(directory))
	run := func(args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		defer cancel()
		output, err := exec.CommandContext(ctx, filepath.Join(bin, "justray"), args...).CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %s, %v", args, output, err)
		}
		return string(output)
	}
	t.Cleanup(func() { _ = client.Shutdown(context.Background()) })
	start := func() <-chan struct{} {
		t.Helper()
		cmd := exec.Command(filepath.Join(bin, "justrayd"))
		cmd.Stderr = t.Output()
		requireNoError(t, cmd.Start())
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		t.Cleanup(func() { _ = cmd.Process.Kill(); <-done })
		return done
	}
	ready := func(connected bool) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		for {
			snapshot, err := client.Snapshot(ctx)
			if err == nil {
				if snapshot.Status.Connected != connected {
					t.Fatalf("connection: %+v", snapshot.Status)
				}
				return
			}
			if ctx.Err() != nil {
				t.Fatal(err)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	stopped := exec.Command(filepath.Join(bin, "justray"), "stop")
	if output, err := stopped.CombinedOutput(); err == nil || stopped.ProcessState.ExitCode() != 2 {
		t.Fatalf("stop without daemon: %s, %v", output, err)
	}
	run("sub", "add", "socks://127.0.0.1:19090#fixture")
	for _, fixture := range []struct{ name, body, endpoint string }{
		{"ipv6", "socks://[2001:db8::1]:19090#ipv6", "[2001:db8::1]:19090"},
		{"json-ipv6", `{"outbounds":[{"tag":"json-ipv6","type":"socks","server":"[2001:db8::2]","server_port":19091}]}`, "[2001:db8::2]:19091"},
		{"yaml-ipv6", "proxies:\n  - {name: yaml-ipv6, type: socks5, server: '[2001:db8::3]', port: 19092}\n", "[2001:db8::3]:19092"},
	} {
		source := fixture.body
		if !strings.HasPrefix(source, "socks://") {
			feed := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				_, _ = fmt.Fprint(writer, fixture.body)
			}))
			defer feed.Close()
			source = feed.URL
		}
		run("sub", "add", source)
		if output := run("sub", "list"); !strings.Contains(output, fixture.endpoint) {
			t.Errorf("imported endpoint missing from subscription list: %s", output)
		}
		if output := run("up", fixture.name, "--proxy"); !strings.Contains(output, fixture.endpoint) {
			t.Errorf("imported endpoint missing from connection details: %s", output)
		}
		if output := run("logs", "daemon"); !strings.Contains(output, "connected ("+fixture.name+", socks "+fixture.endpoint+")") {
			t.Errorf("imported endpoint missing from connection log: %s", output)
		}
	}
	run("stop")
	certificateServer := httptest.NewTLSServer(nil)
	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateServer.TLS.Certificates[0].Certificate[0]})
	privateKey, err := x509.MarshalPKCS8PrivateKey(certificateServer.TLS.Certificates[0].PrivateKey)
	requireNoError(t, err)
	certificateServer.Close()
	certificatePath := filepath.Join(home, "peer.pem")
	requireNoError(t, os.WriteFile(certificatePath, certificatePEM, 0o600))
	t.Setenv("SSL_CERT_FILE", certificatePath)
	clientKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	requireNoError(t, err)
	serverKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	requireNoError(t, err)
	blockedRequests := make(chan chan struct{}, 1)
	requestStarted := make(chan struct{}, 1)
	var peerPorts, targetPorts [2]int
	var reservations [2]net.PacketConn
	for index, protocol := range []string{"wireguard", "hysteria2"} {
		target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			time.Sleep(25 * time.Millisecond)
			select {
			case release := <-blockedRequests:
				requestStarted <- struct{}{}
				select {
				case <-release:
				case <-request.Context().Done():
					return
				}
			default:
			}
			writer.Header().Set("X-Justray-Peer", protocol)
			writer.WriteHeader(http.StatusNoContent)
		}))
		defer target.Close()
		targetPorts[index] = target.Listener.Addr().(*net.TCPAddr).Port
		packet, err := net.ListenPacket("udp", "0.0.0.0:0")
		requireNoError(t, err)
		defer func() { _ = packet.Close() }()
		reservations[index] = packet
		peerPorts[index] = packet.LocalAddr().(*net.UDPAddr).Port
	}
	inboundRegistry := inbound.NewRegistry()
	hysteria2.RegisterInbound(inboundRegistry)
	outboundRegistry := outbound.NewRegistry()
	direct.RegisterOutbound(outboundRegistry)
	endpointRegistry := endpoint.NewRegistry()
	wireguard.RegisterEndpoint(endpointRegistry)
	dnsRegistry := dns.NewTransportRegistry()
	local.RegisterTransport(dnsRegistry)
	peerContext := sbox.Context(t.Context(), inboundRegistry, outboundRegistry,
		endpointRegistry, dnsRegistry, boxservice.NewRegistry(), certificate.NewRegistry())
	peerOptions, err := json.UnmarshalExtendedContext[option.Options](peerContext, fmt.Appendf(nil, `{
		"outbounds":[{"type":"direct","tag":"direct"}],
		"route":{"rules":[
			{"inbound":"wireguard","action":"route","outbound":"direct","override_address":"127.0.0.1","override_port":%d},
			{"inbound":"hysteria2","action":"route","outbound":"direct","override_address":"127.0.0.1","override_port":%d}
		]},
		"endpoints":[{"type":"wireguard","tag":"wireguard","private_key":%q,"listen_port":%d,"workers":1,
			"address":["10.0.0.1/32"],"peers":[{"public_key":%q,"allowed_ips":["10.0.0.2/32"]}]}],
		"inbounds":[{"type":"hysteria2","tag":"hysteria2","listen":"0.0.0.0","listen_port":%d,
			"users":[{"password":"test-password"}],"tls":{"enabled":true,"certificate":%q,"key":%q}}]
	}`, targetPorts[0], targetPorts[1], base64.StdEncoding.EncodeToString(serverKey.Bytes()), peerPorts[0],
		base64.StdEncoding.EncodeToString(clientKey.PublicKey().Bytes()), peerPorts[1], certificatePEM,
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKey})))
	requireNoError(t, err)
	peer, err := sbox.New(sbox.Options{Options: peerOptions, Context: peerContext})
	requireNoError(t, err)
	defer func() { _ = peer.Close() }()
	for _, packet := range reservations {
		requireNoError(t, packet.Close())
	}
	requireNoError(t, peer.Start())
	peerAddress := ""
	if networkInterface := peer.Network().InterfaceMonitor().DefaultInterface(); networkInterface != nil {
		for _, address := range networkInterface.Addresses {
			if address.Addr().Is4() {
				peerAddress = address.Addr().String()
				break
			}
		}
	}
	if peerAddress == "" {
		t.Fatal("test peer requires an IPv4 address on the default interface")
	}
	peerNodes := []string{
		fmt.Sprintf(`{"type":"wireguard","tag":"runtime-wg","server":%q,"server_port":%d,"private_key":%q,"peer_public_key":%q,"local_address":["10.0.0.2/32"]}`,
			peerAddress, peerPorts[0], base64.StdEncoding.EncodeToString(clientKey.Bytes()), base64.StdEncoding.EncodeToString(serverKey.PublicKey().Bytes())),
		fmt.Sprintf(`{"type":"hysteria2","tag":"runtime-hy2","server":%q,"server_port":%d,"password":"test-password","up_mbps":100,"down_mbps":100,"tls":{"enabled":true,"server_name":"example.com"}}`, peerAddress, peerPorts[1]),
	}
	peerNodes = append(peerNodes, strings.Replace(peerNodes[0], "runtime-wg", "duplicate-peer", 1))
	feed := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(writer, `{"outbounds":[%s]}`, strings.Join(peerNodes, ","))
	}))
	defer feed.Close()
	run("sub", "add", feed.URL)
	snapshot, err := client.Snapshot(t.Context())
	requireNoError(t, err)
	peerSubscription := ""
	for _, node := range snapshot.Nodes {
		if node.Name == "runtime-wg" {
			peerSubscription = node.SubscriptionID
		}
	}
	if peerSubscription == "" {
		t.Fatal("WG peer missing from subscription")
	}
	settings := snapshot.Settings
	settings.ProbeURL = "http://198.51.100.7/"
	requireNoError(t, client.SetSettings(t.Context(), snapshot.Settings, settings))
	trafficClient := &http.Client{Transport: &http.Transport{
		Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", port)}), DisableKeepAlives: true,
	}, Timeout: 5 * time.Second}
	defer trafficClient.CloseIdleConnections()
	traffic := func(protocol string) {
		t.Helper()
		response, err := trafficClient.Head(settings.ProbeURL)
		requireNoError(t, err)
		_ = response.Body.Close()
		if response.StatusCode != http.StatusNoContent || response.Header.Get("X-Justray-Peer") != protocol {
			t.Fatalf("traffic: %s, peer=%q, expected=%q", response.Status, response.Header.Get("X-Justray-Peer"), protocol)
		}
	}
	probe := func(name string) {
		t.Helper()
		if output := run("probe", name); strings.Contains(output, "failed") || strings.Contains(output, "t/o") || !strings.Contains(output, "ms") {
			t.Fatalf("probe through %s: %s", name, output)
		}
		snapshot, err := client.Snapshot(t.Context())
		requireNoError(t, err)
		for _, result := range snapshot.Nodes {
			if result.SubscriptionID == peerSubscription && (name == peerSubscription || result.Name == name) && (!result.Probed || result.Probing || !result.Alive) {
				t.Fatalf("missing successful probe result: %+v", result)
			}
		}
	}
	run("down")
	probe(peerSubscription)
	for _, node := range []struct{ name, protocol string }{
		{"runtime-hy2", "hysteria2"}, {"runtime-wg", "wireguard"},
	} {
		run("up", node.name, "--proxy")
		traffic(node.protocol)
		probes := []string{node.name, peerSubscription}
		if node.protocol == "wireguard" {
			probes = append(probes, "duplicate-peer")
		}
		for _, name := range probes {
			probe(name)
		}
		traffic(node.protocol)
	}
	for _, transition := range []struct{ probe, target, protocol string }{
		{"runtime-wg", "runtime-hy2", "hysteria2"},
		{"duplicate-peer", "runtime-wg", "wireguard"},
	} {
		release := make(chan struct{})
		blockedRequests <- release
		probeResult := make(chan string, 1)
		probeContext, probeCancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer probeCancel()
		go func() {
			output, err := exec.CommandContext(probeContext, filepath.Join(bin, "justray"), "probe", transition.probe).CombinedOutput()
			probeResult <- fmt.Sprintf("%s: %v", output, err)
		}()
		select {
		case <-requestStarted:
		case <-probeContext.Done():
			t.Fatal("WG probe did not reach the server")
		}
		run("up", transition.target, "--proxy")
		close(release)
		if output := <-probeResult; !strings.Contains(output, "canceled") {
			t.Fatalf("probe result after switching nodes: %s", output)
		}
		traffic(transition.protocol)
	}
	run("up", "fixture", "--proxy")
	run("stop")
	start()
	ready(true)
	run("down")
	run("stop")
	start()
	ready(false)
	run("up", "--proxy")
	run("stop")

	listener, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	requireNoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	start()
	ready(false)
	if output, err := exec.Command(filepath.Join(bin, "justray"), "status").CombinedOutput(); err == nil {
		t.Fatalf("restore failure reported success: %s", output)
	}
	_ = listener.Close()
	run("up", "--proxy")
	run("stop")

	if runtime.GOOS != "linux" || !elevate.Needed(syscall.EPERM) {
		return
	}
	disk := store.Disk{Dir: directory}
	state, err := disk.Load()
	requireNoError(t, err)
	state.Tun = true
	requireNoError(t, disk.SaveState(state))
	auth := filepath.Join(home, "auth")
	t.Setenv("TEST_AUTH", auth)
	requireNoError(t, os.WriteFile(filepath.Join(bin, "pkexec"), []byte("#!/bin/sh\necho request >> \"$TEST_AUTH\"\nwhile [ ! -f \"$TEST_AUTH.release\" ]; do sleep 0.02; done\nexit 1\n"), 0o700))
	t.Cleanup(func() { _ = os.WriteFile(auth+".release", nil, 0o600) })
	start()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(auth); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("daemon did not request elevation")
		}
		time.Sleep(20 * time.Millisecond)
	}
	statusCtx, statusCancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer statusCancel()
	statusResult := make(chan string, 1)
	go func() {
		output, err := exec.CommandContext(statusCtx, filepath.Join(bin, "justray"), "status").CombinedOutput()
		statusResult <- fmt.Sprintf("%s: %v", output, err)
	}()
	duplicate := start()
	select {
	case <-duplicate:
		t.Fatal("duplicate exited before elevation completed")
	case <-time.After(700 * time.Millisecond):
	}
	select {
	case result := <-statusResult:
		t.Fatalf("client did not wait for authorization: %s", result)
	default:
	}
	requireNoError(t, os.WriteFile(auth+".release", nil, 0o600))
	if result := <-statusResult; !strings.Contains(result, "could not grant permissions") {
		t.Fatalf("waiting client: %s", result)
	}
	ready(false)
	select {
	case <-duplicate:
	case <-time.After(10 * time.Second):
		t.Fatal("duplicate did not exit")
	}
	run("up", "--proxy")
	data, err := os.ReadFile(auth)
	if err != nil || string(data) != "request\n" {
		t.Fatalf("elevation requests: %q, %v", data, err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		output, err := exec.CommandContext(ctx, filepath.Join(bin, "justray"), "up", "--tun").CombinedOutput()
		cancel()
		if err == nil || !strings.Contains(string(output), "could not grant permissions") {
			t.Fatalf("runtime elevation failure: %s, %v", output, err)
		}
		ready(true)
		data, err = os.ReadFile(auth)
		if err != nil || string(data) != strings.Repeat("request\n", attempt+2) {
			t.Fatalf("repeated elevation: %q, %v", data, err)
		}
	}
	run("stop")
	start()
	ready(true)
	run("status")
	run("stop")
}
