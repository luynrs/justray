package protocols

import (
	"cmp"
	"fmt"
	"strings"

	"github.com/luynrs/justray/internal/shared/domain"
)

// shadowtls://password@host:port?version=3&sni=example.com#remark
func ParseShadowTLS(uri string) (domain.Node, error) {
	u, host, port, err := parseURL("shadowtls", uri)
	if err != nil {
		return domain.Node{}, err
	}
	password := ""
	if u.User != nil {
		password = u.User.Username()
		if p, ok := u.User.Password(); ok {
			password = p
		}
	}
	if password == "" {
		return domain.Node{}, fmt.Errorf("shadowtls: missing password")
	}
	q := u.Query()
	return domain.Node{
		Name:     cmp.Or(u.Fragment, host),
		Protocol: domain.Shadow,
		Server:   host,
		Port:     port,
		TLS:      tlsFrom(q, host),
		ShadowTLS: &domain.ShadowTLS{
			Version:  cmp.Or(atoi(q.Get("version")), 3),
			Password: password,
			SNI:      cmp.Or(q.Get("sni"), q.Get("host"), host),
		},
	}, nil
}

func parseShadowTLSPlugin(raw, host string) (*domain.ShadowTLS, error) {
	base, opts, _ := strings.Cut(raw, ";")
	if base != "shadow-tls" {
		return nil, fmt.Errorf("unsupported plugin %q", base)
	}
	stls := &domain.ShadowTLS{Version: 3, SNI: host}
	for option := range strings.SplitSeq(opts, ";") {
		key, value, ok := strings.Cut(option, "=")
		if !ok {
			continue
		}
		switch key {
		case "host":
			stls.SNI = value
		case "password":
			stls.Password = value
		case "version":
			stls.Version = atoi(value)
		}
	}
	if stls.Password == "" {
		return nil, fmt.Errorf("shadow-tls: missing password")
	}
	return stls, nil
}
