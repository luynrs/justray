package engine

import (
	"context"

	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/engine/outbound"
)

func resolved(ctx context.Context, n domain.Node, s domain.Settings) (domain.Node, error) {
	ip, err := s.Resolve(ctx, n.Server)
	if err != nil {
		return n, err
	}
	return withServerIP(n, ip), nil
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
