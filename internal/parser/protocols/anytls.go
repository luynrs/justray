package protocols

import (
	"cmp"
	"fmt"

	"github.com/luynrs/justray/internal/domain"
)

func ParseAnyTLS(uri string) (domain.Node, error) {
	u, host, port, err := parseURL("anytls", uri)
	if err != nil {
		return domain.Node{}, err
	}
	pw := ""
	if u.User != nil {
		if p, ok := u.User.Password(); ok {
			pw = p
		} else {
			pw = u.User.Username()
		}
	}
	q := u.Query()
	pw = cmp.Or(pw, q.Get("password"), q.Get("auth"))
	if pw == "" {
		return domain.Node{}, fmt.Errorf("anytls: missing password")
	}

	n := domain.Node{
		Name:     cmp.Or(u.Fragment, host),
		Protocol: domain.AnyTLS,
		Server:   host,
		Port:     port,
		Auth:     domain.Auth{Password: pw},
		TLS:      tlsFrom(q, host),
	}
	return n, nil
}
