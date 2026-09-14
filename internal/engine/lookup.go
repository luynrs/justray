package engine

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"time"

	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/engine/outbound"
)

func resolved(ctx context.Context, n domain.Node, s domain.Settings) (domain.Node, error) {
	if _, err := netip.ParseAddr(n.Server); err == nil {
		return n, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	ips, err := resolver(s).LookupNetIP(ctx, network(s), n.Server)
	if err != nil || len(ips) == 0 {
		ips, err = net.DefaultResolver.LookupNetIP(ctx, network(s), n.Server)
	}
	switch {
	case err != nil:
		return n, err
	case len(ips) == 0:
		return n, fmt.Errorf("no addresses for %s", n.Server)
	}

	return withServerIP(n, ips[0].Unmap().String()), nil
}

func resolver(s domain.Settings) *net.Resolver {
	dns := s.DNS
	if u, err := url.Parse(dns); err == nil && u.Hostname() != "" {
		dns = u.Hostname()
	}
	if _, err := netip.ParseAddr(dns); err != nil {
		dns = domain.DefaultDNS
	}
	target := net.JoinHostPort(dns, "53")
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: 1500 * time.Millisecond}
			return d.DialContext(ctx, "udp", target)
		},
	}
}

func withServerIP(n domain.Node, ip string) domain.Node {
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
	return n
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
