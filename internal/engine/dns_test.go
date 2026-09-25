package engine

import (
	"encoding/pem"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/miekg/dns"
	"github.com/sagernet/sing-box/adapter"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json/badoption"
	"github.com/sagernet/sing/service"

	"github.com/luynrs/justray/internal/domain"
)

func TestDNSResolution(t *testing.T) {
	answer := func(query *dns.Msg) *dns.Msg {
		response := new(dns.Msg)
		response.SetReply(query)
		if len(query.Question) > 0 && query.Question[0].Qtype == dns.TypeA {
			response.Answer = []dns.RR{&dns.A{
				Hdr: dns.RR_Header{Name: query.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
				A:   net.IPv4(203, 0, 113, 7),
			}}
			if query.Question[0].Name == "proxy.example.test." {
				response.Answer[0].(*dns.A).A = net.IPv4(127, 0, 0, 1)
			}
		}
		return response
	}
	lookup := func(t *testing.T, server string, configure func(*option.Options)) {
		t.Helper()
		settings, _ := (domain.Settings{}).Normalize()
		settings.DNS = server
		settings.IPVersion = "ipv4"
		options := ProbeConfig(t.Context(), nil, settings, "")
		if configure != nil {
			configure(options)
		}
		ctx := Context(t.Context())
		box, err := startBox(ctx, *options)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = box.Close() }()
		addresses, err := service.FromContext[adapter.DNSRouter](ctx).Lookup(t.Context(), "example.test", adapter.DNSQueryOptions{})
		// The initial interface update can reset an in-flight lookup, as in delay.
		if errors.Is(err, net.ErrClosed) {
			addresses, err = service.FromContext[adapter.DNSRouter](ctx).Lookup(t.Context(), "example.test", adapter.DNSQueryOptions{})
		}
		if err != nil || len(addresses) != 1 || addresses[0] != netip.MustParseAddr("203.0.113.7") {
			t.Fatalf("DNS lookup via %s: %v, %v", server, addresses, err)
		}
	}

	packetConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	udpServer := &dns.Server{PacketConn: packetConn, Handler: dns.HandlerFunc(func(writer dns.ResponseWriter, query *dns.Msg) {
		_ = writer.WriteMsg(answer(query))
	})}
	go func() { _ = udpServer.ActivateAndServe() }()
	t.Cleanup(func() { _ = udpServer.Shutdown() })
	t.Run("UDP", func(t *testing.T) { lookup(t, packetConn.LocalAddr().String(), nil) })
	t.Run("TCP detour", func(t *testing.T) {
		var queries atomic.Int32
		proxy := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.Method != http.MethodConnect || request.Host != "203.0.113.53:53" {
				t.Errorf("unexpected proxy request: %s %s", request.Method, request.Host)
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			conn, stream, err := writer.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			defer conn.Close()
			_, _ = stream.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
			if err := stream.Flush(); err != nil {
				t.Error(err)
				return
			}
			connection := &dns.Conn{Conn: conn}
			query, err := connection.ReadMsg()
			if err != nil {
				t.Error(err)
				return
			}
			if query.Question[0].Name != "example.test." {
				t.Errorf("unexpected tunneled lookup: %v", query.Question)
			}
			if query.Question[0].Qtype == dns.TypeA {
				queries.Add(1)
			}
			if err := connection.WriteMsg(answer(query)); err != nil {
				t.Error(err)
			}
		}))
		defer proxy.Close()
		lookup(t, "203.0.113.53", func(options *option.Options) {
			settings, _ := (domain.Settings{}).Normalize()
			settings.DNS, settings.IPVersion = "203.0.113.53", "ipv4"
			built, err := Build(t.Context(), domain.Node{Protocol: domain.HTTP, Server: "proxy.example.test", Port: proxy.Listener.Addr().(*net.TCPAddr).Port}, settings, "", false)
			if err != nil {
				t.Fatal(err)
			}
			built.Inbounds = nil
			built.DNS.Servers[1].Options.(*option.RemoteDNSServerOptions).Server = "127.0.0.1"
			built.DNS.Servers[1].Options.(*option.RemoteDNSServerOptions).ServerPort = uint16(packetConn.LocalAddr().(*net.UDPAddr).Port)
			*options = *built
		})
		if queries.Load() == 0 {
			t.Fatal("DNS lookup bypassed the HTTP proxy")
		}
	})

	dohServer := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/dns-query" || request.Method != http.MethodPost {
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		wire, err := io.ReadAll(request.Body)
		if err != nil {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		query := new(dns.Msg)
		if query.Unpack(wire) != nil {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		wire, err = answer(query).Pack()
		if err != nil {
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		writer.Header().Set("Content-Type", "application/dns-message")
		_, _ = writer.Write(wire)
	}))
	defer dohServer.Close()
	for name, address := range map[string]string{
		"DoH":           dohServer.URL + "/dns-query",
		"DoH bootstrap": strings.Replace(dohServer.URL, "127.0.0.1", "localhost", 1) + "/dns-query",
	} {
		t.Run(name, func(t *testing.T) {
			lookup(t, address, func(options *option.Options) {
				options.DNS.Servers[0].Options.(*option.RemoteHTTPSDNSServerOptions).TLS = &option.OutboundTLSOptions{
					ServerName:  "127.0.0.1",
					Certificate: badoption.Listable[string]{string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: dohServer.Certificate().Raw}))},
				}
			})
		})
	}

	settings, _ := (domain.Settings{}).Normalize()
	settings.DNS = "https://dns.example/dns-query"
	servers := ProbeConfig(t.Context(), nil, settings, "").DNS.Servers
	resolver := servers[0].Options.(*option.RemoteHTTPSDNSServerOptions).DomainResolver
	if len(servers) != 2 || resolver == nil || resolver.Server != "remote-bootstrap" || servers[1].Type != "local" {
		t.Fatalf("DoH hostname bootstrap: %+v", servers)
	}
}

func TestDNSRouting(t *testing.T) {
	settings, _ := (domain.Settings{}).Normalize()
	node := domain.Node{ID: "node", Protocol: domain.VLess, Server: "node.example", Port: 443, Auth: domain.Auth{UUID: "11111111-1111-1111-1111-111111111111"}}
	settings.DNS = "1.1.1.1"
	options, err := Build(t.Context(), node, settings, "", true)
	if err != nil {
		t.Fatal(err)
	}
	servers := options.DNS.Servers
	if len(servers) != 2 || servers[0].Type != C.DNSTypeTCP || servers[0].Options.(*option.RemoteDNSServerOptions).Detour != Tag ||
		servers[1].Type != C.DNSTypeUDP || servers[1].Options.(*option.RemoteDNSServerOptions).Detour != "" || options.Route.DefaultDomainResolver.Server != "node" {
		t.Fatalf("proxy DNS routing: %+v", servers)
	}
	if len(options.Inbounds) != 2 || options.Inbounds[1].Type != C.TypeTun {
		t.Fatalf("TUN inbound missing: %+v", options.Inbounds)
	}
	hijacked := false
	for _, rule := range options.Route.Rules {
		if rule.DefaultOptions.RuleAction.Action == C.RuleActionTypeHijackDNS {
			hijacked = true
		}
	}
	if !hijacked {
		t.Fatal("TUN DNS hijack rule missing")
	}

	settings.DNS = "https://dns.example/dns-query"
	options, err = Build(t.Context(), node, settings, "", false)
	if err != nil {
		t.Fatal(err)
	}
	servers = options.DNS.Servers
	if len(servers) != 4 || servers[0].Type != C.DNSTypeHTTPS || servers[1].Type != C.DNSTypeLocal ||
		servers[2].Type != C.DNSTypeHTTPS || servers[3].Type != C.DNSTypeLocal ||
		servers[0].Options.(*option.RemoteHTTPSDNSServerOptions).DomainResolver.Server != "remote-bootstrap" ||
		servers[2].Options.(*option.RemoteHTTPSDNSServerOptions).DomainResolver.Server != "node-bootstrap" ||
		options.Route.DefaultDomainResolver.Server != "node" {
		t.Fatalf("DoH bootstrap routing: %+v", servers)
	}

	settings.Mode = domain.DirectAll
	options, err = Build(t.Context(), node, settings, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(options.DNS.Servers) != 2 || options.Route.DefaultDomainResolver.Server != "remote" || options.Route.Final != "direct" {
		t.Fatalf("direct DNS routing: %+v", options.DNS.Servers)
	}
}
