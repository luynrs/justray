package main

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
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
	build := exec.Command("go", "build", "-tags=with_quic,with_utls,with_gvisor,with_grpc,with_xhttp,with_awg,badlinkname", "-ldflags=-s -w", "-o", bin+string(os.PathSeparator), "./cmd/justray", "./cmd/justrayd")
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
	var peerPorts, targetPorts [4]int
	var reservations [4]net.PacketConn
	for index, protocol := range []string{"wireguard", "hysteria2", "awg2", "awg3"} {
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
	const awg2 = `"jc":4,"jmin":8,"jmax":80,"s1":28,"s2":121,"s3":25,"s4":12,"h1":"1888111000-1888111100","h2":"1888122000-1888122100","h3":"1888133000-1888133100","h4":"1888222333-1888222444","i1":"<b 0xf6>","i2":"<r 8>","i3":"<rc 8>","i4":"<rd 8>","i5":"<t>"`
	const awg3 = `"jc":4,"jmin":10,"jmax":50,"s1":55,"s2":42,"s3":40,"s4":12,"h1":1,"h2":2,"h3":3,"h4":4,"header_protection_key":"pBNbQ3vIFMTZCbCkQtLObNUtsM9hAi+ZhlrgZbH0Xgc=","content_padding_addition":"10-100","rekey_after_time":"100-120","rekey_timeout":"3-7","reject_after_time":"150-180","keepalive_timeout":"5-15","max_handshake_attempts":"15-20","random_trailers":true,"disable_cookies":true`
	peerOptions, err := json.UnmarshalExtendedContext[option.Options](peerContext, fmt.Appendf(nil, `{
		"outbounds":[{"type":"direct","tag":"direct"}],
		"route":{"rules":[
			{"inbound":"wireguard","action":"route","outbound":"direct","override_address":"127.0.0.1","override_port":%d},
			{"inbound":"hysteria2","action":"route","outbound":"direct","override_address":"127.0.0.1","override_port":%d},
			{"inbound":"awg2","action":"route","outbound":"direct","override_address":"127.0.0.1","override_port":%d},
			{"inbound":"awg3","action":"route","outbound":"direct","override_address":"127.0.0.1","override_port":%d}
		]},
		"endpoints":[{"type":"wireguard","tag":"wireguard","private_key":%q,"listen_port":%d,"workers":1,
			"address":["10.0.0.1/32"],"peers":[{"public_key":%q,"allowed_ips":["10.0.0.2/32"]}]},
			{"type":"wireguard","tag":"awg2","private_key":%q,"listen_port":%d,"workers":1,%s,
			"address":["10.0.0.1/32"],"peers":[{"public_key":%q,"allowed_ips":["10.0.0.2/32"]}]},
			{"type":"wireguard","tag":"awg3","private_key":%q,"listen_port":%d,"workers":1,%s,
			"address":["10.0.0.1/32"],"peers":[{"public_key":%q,"allowed_ips":["10.0.0.2/32"]}]}],
		"inbounds":[{"type":"hysteria2","tag":"hysteria2","listen":"0.0.0.0","listen_port":%d,
			"users":[{"password":"test-password"}],"tls":{"enabled":true,"certificate":%q,"key":%q}}]
	}`, targetPorts[0], targetPorts[1], targetPorts[2], targetPorts[3], base64.StdEncoding.EncodeToString(serverKey.Bytes()), peerPorts[0],
		base64.StdEncoding.EncodeToString(clientKey.PublicKey().Bytes()),
		base64.StdEncoding.EncodeToString(serverKey.Bytes()), peerPorts[2], awg2, base64.StdEncoding.EncodeToString(clientKey.PublicKey().Bytes()),
		base64.StdEncoding.EncodeToString(serverKey.Bytes()), peerPorts[3], awg3, base64.StdEncoding.EncodeToString(clientKey.PublicKey().Bytes()),
		peerPorts[1], certificatePEM,
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
		t.Skip("requires an IPv4 address on the default network interface")
	}
	peerNodes := []string{
		fmt.Sprintf(`{"type":"wireguard","tag":"runtime-wg","server":%q,"server_port":%d,"private_key":%q,"peer_public_key":%q,"local_address":["10.0.0.2/32"]}`,
			peerAddress, peerPorts[0], base64.StdEncoding.EncodeToString(clientKey.Bytes()), base64.StdEncoding.EncodeToString(serverKey.PublicKey().Bytes())),
		fmt.Sprintf(`{"type":"hysteria2","tag":"runtime-hy2","server":%q,"server_port":%d,"password":"test-password","up_mbps":100,"down_mbps":100,"tls":{"enabled":true,"server_name":"example.com"}}`, peerAddress, peerPorts[1]),
		fmt.Sprintf(`{"type":"wireguard","tag":"runtime-awg2","server":%q,"server_port":%d,"private_key":%q,"peer_public_key":%q,"local_address":["10.0.0.2/32"],%s}`,
			peerAddress, peerPorts[2], base64.StdEncoding.EncodeToString(clientKey.Bytes()), base64.StdEncoding.EncodeToString(serverKey.PublicKey().Bytes()), awg2),
		fmt.Sprintf(`{"type":"wireguard","tag":"runtime-awg3","private_key":%q,"address":["10.0.0.2/32"],"peers":[{"address":%q,"port":%d,"public_key":%q,"persistent_keepalive_interval":"25-35"}],%s}`,
			base64.StdEncoding.EncodeToString(clientKey.Bytes()), peerAddress, peerPorts[3], base64.StdEncoding.EncodeToString(serverKey.PublicKey().Bytes()), awg3),
	}
	peerNodes = append(peerNodes, strings.Replace(peerNodes[0], "runtime-wg", "duplicate-peer", 1))
	feed := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(writer, `{"outbounds":[%s]}`, strings.Join(peerNodes, ","))
	}))
	defer feed.Close()
	run("sub", "add", feed.URL)
	for _, content := range []string{
		fmt.Sprintf("proxies:\n  - {name: yaml-awg2, type: wireguard, server: %q, port: %d, private-key: %q, public-key: %q, ip: 10.0.0.2, amnezia-wg-option: {%s}}",
			peerAddress, peerPorts[2], base64.StdEncoding.EncodeToString(clientKey.Bytes()), base64.StdEncoding.EncodeToString(serverKey.PublicKey().Bytes()), awg2),
		fmt.Sprintf("proxies:\n  - {name: yaml-awg3, type: wireguard, server: %q, port: %d, private-key: %q, public-key: %q, ip: 10.0.0.2, persistent-keepalive: '25-35', amnezia-wg-option: {%s}}",
			peerAddress, peerPorts[3], base64.StdEncoding.EncodeToString(clientKey.Bytes()), base64.StdEncoding.EncodeToString(serverKey.PublicKey().Bytes()), strings.ReplaceAll(awg3, "_", "-")),
	} {
		feed := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprint(writer, content)
		}))
		defer feed.Close()
		run("sub", "add", feed.URL)
	}
	uriOptions := url.Values{"address": {"10.0.0.2/32"}, "publickey": {base64.StdEncoding.EncodeToString(serverKey.PublicKey().Bytes())}}
	var awgFields map[string]any
	requireNoError(t, json.Unmarshal([]byte("{"+awg2+"}"), &awgFields))
	for name, value := range awgFields {
		uriOptions.Set(name, fmt.Sprint(value))
	}
	run("sub", "add", (&url.URL{Scheme: "wireguard", User: url.User(base64.StdEncoding.EncodeToString(clientKey.Bytes())), Host: fmt.Sprintf("%s:%d", peerAddress, peerPorts[2]), RawQuery: uriOptions.Encode(), Fragment: "uri-awg2"}).String())
	clientConfig := fmt.Sprintf(`{"client_priv_key":%q,"server_pub_key":%q,"client_ip":"10.0.0.2","persistent_keep_alive":"25-35","Jc":"4","Jmin":"10","Jmax":"50","S1":"55","S2":"42","S3":"40","S4":"12","H1":"1","H2":"2","H3":"3","H4":"4","HeaderProtectionKey":"pBNbQ3vIFMTZCbCkQtLObNUtsM9hAi+ZhlrgZbH0Xgc=","ContentPaddingAddition":"10-100","RekeyAfterTime":"100-120","RekeyTimeout":"3-7","RejectAfterTime":"150-180","KeepaliveTimeout":"5-15","MaxHandshakeAttempts":"15-20","RandomTrailers":"on","DisableCookies":"on"}`,
		base64.StdEncoding.EncodeToString(clientKey.Bytes()), base64.StdEncoding.EncodeToString(serverKey.PublicKey().Bytes()))
	for _, compressed := range []bool{false, true} {
		content := fmt.Appendf(nil, `{"format_version":1,"description":"vpn-awg3-%t","hostName":%q,"defaultContainer":"amnezia-awg2","containers":[{"container":"amnezia-awg2","awg":{"port":"%d","last_config":%q}}]}`, compressed, peerAddress, peerPorts[3], clientConfig)
		if compressed {
			encoded := bytes.NewBuffer(binary.BigEndian.AppendUint32(nil, uint32(len(content))))
			compressor := zlib.NewWriter(encoded)
			_, err := compressor.Write(content)
			requireNoError(t, err)
			requireNoError(t, compressor.Close())
			content = encoded.Bytes()
		}
		run("sub", "add", "vpn://"+base64.RawURLEncoding.EncodeToString(content))
	}
	clientConfig = fmt.Sprintf(`{"hostName":%q,"port":%d,"client_priv_key":%q,"server_pub_key":%q,"client_ip":"10.0.0.2"}`,
		peerAddress, peerPorts[0], base64.StdEncoding.EncodeToString(clientKey.Bytes()), base64.StdEncoding.EncodeToString(serverKey.PublicKey().Bytes()))
	wireguardProfile := fmt.Appendf(nil, `{"description":"vpn-wg","hostName":"ignored.example","port":22,"defaultContainer":"amnezia-wireguard","containers":[{"container":"amnezia-openvpn"},{"container":"amnezia-wireguard","wireguard":{"port":"1","last_config":%q}}]}`, clientConfig)
	run("sub", "add", "vpn://"+base64.RawURLEncoding.EncodeToString(wireguardProfile))
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
		{"vpn-wg", "wireguard"},
		{"runtime-awg2", "awg2"}, {"yaml-awg2", "awg2"}, {"uri-awg2", "awg2"},
		{"runtime-awg3", "awg3"}, {"yaml-awg3", "awg3"}, {"vpn-awg3-false", "awg3"}, {"vpn-awg3-true", "awg3"},
	} {
		protocol := node.protocol
		if strings.HasPrefix(protocol, "awg") {
			protocol = "amneziawg"
		}
		if output := run("up", node.name, "--proxy"); !strings.Contains(output, protocol) {
			t.Fatalf("connection protocol %s missing: %s", protocol, output)
		}
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
	uriOptions.Set("jmin", "81")
	run("sub", "add", (&url.URL{Scheme: "wireguard", User: url.User(base64.StdEncoding.EncodeToString(clientKey.Bytes())), Host: fmt.Sprintf("%s:%d", peerAddress, peerPorts[2]), RawQuery: uriOptions.Encode(), Fragment: "invalid-awg"}).String())
	failureContext, failureCancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer failureCancel()
	if output, err := exec.CommandContext(failureContext, filepath.Join(bin, "justray"), "up", "invalid-awg", "--proxy").CombinedOutput(); err == nil || !strings.Contains(string(output), "jmin") {
		t.Fatalf("invalid AWG junk range: %s, %v", output, err)
	}
	traffic("awg3")
	run("stop")
	start()
	ready(true)
	traffic("awg3")
	for _, transition := range []struct{ probe, target, protocol string }{
		{"runtime-wg", "runtime-hy2", "hysteria2"},
		{"duplicate-peer", "runtime-wg", "wireguard"},
	} {
		release := make(chan struct{})
		blockedRequests <- release
		probeResult := make(chan error, 1)
		probeContext, probeCancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer probeCancel()
		go func() {
			output, err := exec.CommandContext(probeContext, filepath.Join(bin, "justray"), "probe", transition.probe).CombinedOutput()
			if len(output) != 0 {
				err = fmt.Errorf("unexpected probe output: %s (%v)", output, err)
			}
			probeResult <- err
		}()
		select {
		case <-requestStarted:
		case <-probeContext.Done():
			t.Fatal("WG probe did not reach the server")
		}
		run("up", transition.target, "--proxy")
		close(release)
		if err := <-probeResult; err != nil {
			t.Fatalf("probe after switching nodes: %v", err)
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
