package protocols

import (
	"cmp"
	"fmt"

	"github.com/luynrs/justray/internal/shared/domain"
)

func ParseAnyTLS(uri string) (domain.Node, error) {
	u, host, port, err := parseURL("anytls", uri)
	if err != nil {
		return domain.Node{}, err
	}
	if u.User == nil || u.User.Username() == "" {
		return domain.Node{}, fmt.Errorf("anytls: missing password")
	}

	q := u.Query()
	n := domain.Node{
		Name:     cmp.Or(u.Fragment, host),
		Protocol: domain.AnyTLS,
		Server:   host,
		Port:     port,
		Auth:     domain.Auth{Password: u.User.Username()},
		TLS:      tlsFrom(q, host),
	}
	return n, nil
}
