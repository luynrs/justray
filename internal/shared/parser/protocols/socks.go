package protocols

import (
	"cmp"

	"github.com/luynrs/justray/internal/shared/domain"
)

func ParseSOCKS(uri string) (domain.Node, error) {
	u, host, port, err := parseURL("socks", uri)
	if err != nil {
		return domain.Node{}, err
	}

	var user, password string
	if u.User != nil {
		user = u.User.Username()
		if p, ok := u.User.Password(); ok {
			password = p
		} else {
			user, password = splitCreds(user)
		}
	}
	n := domain.Node{
		Name:     cmp.Or(u.Fragment, host),
		Protocol: domain.SOCKS,
		Server:   host,
		Port:     port,
		Auth:     domain.Auth{Username: user, Password: password},
	}
	return n, nil
}
