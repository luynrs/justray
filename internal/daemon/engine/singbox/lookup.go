package singbox

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"time"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"

	"github.com/luynrs/justray/internal/daemon/engine/singbox/outbound"
	"github.com/luynrs/justray/internal/shared/domain"
)

var (
	dnsMu    sync.Mutex
	dnsCache = map[string]dnsEntry{}
)

type dnsEntry struct {
	ip  string
	exp time.Time
}

func resolved(n domain.Node, s domain.Settings) (domain.Node, error) {
	if _, err := netip.ParseAddr(n.Server); err == nil {
		return n, nil
	}
	ip, err := lookup(n.Server, s)
	if err != nil {
		return n, err
	}
	switch {
	case n.TLS != nil && n.TLS.SNI == "":
		tls := *n.TLS
		tls.SNI = n.Server
		n.TLS = &tls
	case n.TLS == nil && outbound.TLSOnly(n.Protocol):
		n.TLS = &domain.TLS{SNI: n.Server}
	}
	if n.Transport.Host == "" {
		n.Transport.Host = n.Server
	}
	n.Server = ip
	return n, nil
}

func dnsKey(host string, s domain.Settings) string { return s.IPVersion + ":" + host }

func forget(host string, s domain.Settings) {
	dnsMu.Lock()
	delete(dnsCache, dnsKey(host, s))
	dnsMu.Unlock()
}

func lookup(host string, s domain.Settings) (string, error) {
	key := dnsKey(host, s)

	dnsMu.Lock()
	e, ok := dnsCache[key]
	dnsMu.Unlock()
	if ok && time.Now().Before(e.exp) {
		return e.ip, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	resolver := net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(s.DNS, "53"))
		},
	}
	ips, err := resolver.LookupNetIP(ctx, network(s), host)
	switch {
	case err != nil:
		return "", fmt.Errorf("could not resolve %s: %w", host, err)
	case len(ips) == 0:
		return "", fmt.Errorf("no addresses for %s", host)
	}

	e = dnsEntry{ips[0].Unmap().String(), time.Now().Add(10 * time.Minute)} // ttl
	dnsMu.Lock()
	dnsCache[key] = e
	dnsMu.Unlock()
	return e.ip, nil
}

func network(s domain.Settings) string {
	switch s.IPVersion {
	case "ipv4":
		return "ip4"
	case "ipv6":
		return "ip6"
	}
	return "ip"
}

var dnsStrategy = map[string]option.DomainStrategy{
	"auto": option.DomainStrategy(C.DomainStrategyPreferIPv4),
	"ipv4": option.DomainStrategy(C.DomainStrategyIPv4Only),
	"ipv6": option.DomainStrategy(C.DomainStrategyIPv6Only),
}
