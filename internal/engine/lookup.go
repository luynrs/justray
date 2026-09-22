package engine

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"
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

	var ips []netip.Addr
	if !strings.HasPrefix(s.DNS, "https://") {
		cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		ips, _ = udpResolver(s.DNS).LookupNetIP(cctx, network(s), n.Server)
		cancel()
	}
	if len(ips) == 0 {
		var err error
		if ips, err = net.DefaultResolver.LookupNetIP(ctx, network(s), n.Server); err != nil {
			return n, err
		}
	}
	if len(ips) == 0 {
		return n, fmt.Errorf("no addresses for %s", n.Server)
	}
	return withServerIP(n, ips[0].Unmap().String()), nil
}

func udpResolver(dns string) *net.Resolver {
	if dns == "" {
		dns = domain.DefaultDNS
	}
	if _, _, err := net.SplitHostPort(dns); err != nil {
		dns = net.JoinHostPort(dns, "53")
	}
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, dns)
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
