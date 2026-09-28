package outbound

import (
	"github.com/sagernet/sing-box/option"

	"github.com/luynrs/justray/internal/domain"
)

func tlsOptions(n domain.Node) *option.OutboundTLSOptions {
	if n.TLS == nil && n.Reality == nil {
		return nil
	}
	tls := &option.OutboundTLSOptions{Enabled: true}
	if n.Reality != nil {
		tls.Reality = &option.OutboundRealityOptions{
			Enabled:   true,
			PublicKey: n.Reality.PublicKey,
			ShortID:   n.Reality.ShortID,
		}
		tls.UTLS = &option.OutboundUTLSOptions{Enabled: true, Fingerprint: "chrome"}
	}
	if n.TLS != nil {
		tls.ServerName = n.TLS.SNI
		tls.Insecure = n.TLS.Insecure
		tls.ALPN = n.TLS.ALPN
		if n.TLS.Fingerprint != "" {
			tls.UTLS = &option.OutboundUTLSOptions{Enabled: true, Fingerprint: n.TLS.Fingerprint}
		}
	}
	return tls
}
