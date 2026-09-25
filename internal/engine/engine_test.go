package engine

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/luynrs/justray/internal/domain"
)

func TestRebuilds(t *testing.T) {
	settings, _ := (domain.Settings{}).Normalize()
	for name, change := range map[string]func(*domain.Settings){
		"port":       func(next *domain.Settings) { next.Port++ },
		"DNS":        func(next *domain.Settings) { next.DNS = "1.1.1.1" },
		"DNS hijack": func(next *domain.Settings) { next.DNSHijack = "off" },
		"routing":    func(next *domain.Settings) { next.Mode = domain.DirectAll },
		"rules":      func(next *domain.Settings) { next.Block = []string{"example.test"} },
	} {
		changed := settings
		change(&changed)
		if !Rebuilds(settings, changed) {
			t.Errorf("%s change did not restart the engine", name)
		}
	}
	for name, change := range map[string]func(*domain.Settings){
		"probe":   func(next *domain.Settings) { next.ProbeURL = "https://example.test/check" },
		"refresh": func(next *domain.Settings) { next.RefreshEvery++ },
		"display": func(next *domain.Settings) { next.Emoji = "on" },
	} {
		changed := settings
		change(&changed)
		if Rebuilds(settings, changed) {
			t.Errorf("%s change restarted the engine", name)
		}
	}
}

func TestNodeSwitch(t *testing.T) {
	firstProxy, firstCalls := testHTTPProxy(t)
	defer firstProxy.Close()
	secondProxy, secondCalls := testHTTPProxy(t)
	defer secondProxy.Close()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	settings, _ := (domain.Settings{}).Normalize()
	settings.Port = listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	proxyNode := func(id, address string) domain.Node {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			t.Fatal(err)
		}
		number, err := strconv.Atoi(port)
		if err != nil {
			t.Fatal(err)
		}
		return domain.Node{ID: id, Protocol: domain.HTTP, Server: host, Port: number}
	}
	first := proxyNode("first", firstProxy.Listener.Addr().String())
	second := proxyNode("second", secondProxy.Listener.Addr().String())
	box := &Box{lifetime: t.Context()}
	defer func() { _ = box.Stop() }()
	if err := box.Apply(t.Context(), SessionSpec{Node: first, Settings: settings}); err != nil {
		t.Fatal(err)
	}
	request := func() {
		t.Helper()
		outbound, ok := box.inst.Outbound().Outbound(Tag)
		if !ok {
			t.Fatal("proxy outbound missing")
		}
		_, err := delay(t.Context(), outbound, "http://example.test/")
		if err != nil {
			t.Fatal(err)
		}
	}
	request()
	instance := box.inst
	if err := box.Apply(t.Context(), SessionSpec{Node: second, Settings: settings}); err != nil {
		t.Fatal(err)
	}
	request()
	if box.inst != instance || firstCalls.Load() != 1 || secondCalls.Load() != 1 {
		t.Fatalf("node switch used wrong proxy: first=%d, second=%d", firstCalls.Load(), secondCalls.Load())
	}
	broken := second
	broken.ID = "broken"
	broken.Protocol = "unknown"
	if err := box.Apply(t.Context(), SessionSpec{Node: broken, Settings: settings}); err == nil {
		t.Fatal("invalid node switch succeeded")
	}
	request()
	if box.node.ID != second.ID || secondCalls.Load() != 2 {
		t.Fatal("failed node switch lost the working proxy")
	}
}

func testHTTPProxy(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	calls := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodConnect {
			writer.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		conn, stream, err := writer.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = conn.Close() }()
		if _, err := stream.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
			t.Error(err)
			return
		}
		if err := stream.Flush(); err != nil {
			t.Error(err)
			return
		}
		if _, err := http.ReadRequest(stream.Reader); err != nil {
			t.Error(err)
			return
		}
		calls.Add(1)
		_, _ = stream.WriteString("HTTP/1.1 204 No Content\r\nContent-Length: 0\r\n\r\n")
		_ = stream.Flush()
	}))
	return server, calls
}

func TestProbeEngine(t *testing.T) {
	settings, _ := (domain.Settings{}).Normalize()
	settings.ProbeURL = "http://example.test/"
	server, calls := testHTTPProxy(t)
	defer server.Close()
	nodes := []domain.Node{
		{ID: "working", Protocol: domain.HTTP, Server: "127.0.0.1", Port: server.Listener.Addr().(*net.TCPAddr).Port},
		{ID: "valid", Protocol: domain.VLess, Server: "127.0.0.1", Port: 9993, Auth: domain.Auth{UUID: "11111111-1111-1111-1111-111111111111"}},
		{ID: "invalid", Protocol: domain.VLess, Server: "127.0.0.1", Port: 9994, Auth: domain.Auth{UUID: "11111111-1111-1111-1111-111111111111"}, Reality: &domain.Reality{PublicKey: "invalid-key"}},
		{
			ID: "wireguard", Protocol: domain.WG, Server: "127.0.0.1", Port: 51820,
			WireGuard: &domain.WireGuard{
				PrivateKey:    "aGVsbG93b3JsZGhlbGxvd29ybGRoZWxsb3dvcmxkMTI=",
				PeerPublicKey: "aGVsbG93b3JsZGhlbGxvd29ybGRoZWxsb3dvcmxkMTI=",
				Address:       []string{"10.0.0.2/32"},
			},
		},
	}
	var mutex sync.Mutex
	results := make(map[string]Result)
	err := Probe(t.Context(), nodes, settings, "", func(nodeID string, result Result) {
		mutex.Lock()
		results[nodeID] = result
		mutex.Unlock()
	})
	if err != nil {
		t.Fatalf("node failures must not fail the probe: %v", err)
	}
	if len(results) != len(nodes) {
		t.Fatalf("expected %d results, got %d", len(nodes), len(results))
	}
	if !results["working"].Alive || calls.Load() != 1 || results["valid"].Alive || results["invalid"].Alive || results["wireguard"].Alive {
		t.Fatalf("probe results: %+v, proxy calls=%d", results, calls.Load())
	}
	for _, id := range []string{"valid", "invalid", "wireguard"} {
		if results[id] != (Result{}) {
			t.Errorf("failed node %s: %+v", id, results[id])
		}
	}
}
