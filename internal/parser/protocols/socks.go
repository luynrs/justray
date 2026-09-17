package protocols

import (
	"cmp"

	"github.com/luynrs/justray/internal/domain"
)

func ParseSOCKS(uri string) (domain.Node, error) {
	u, host, port, err := parseURL("socks", uri)
	if err != nil {
		return domain.Node{}, err
	}
	user, pw := userPass(u)
	if pw == "" && user != "" {
		user, pw = splitCreds(user)
	}
	return domain.Node{
		Name:     cmp.Or(u.Fragment, host),
		Protocol: domain.SOCKS,
		Server:   host,
		Port:     port,
		Auth:     domain.Auth{Username: user, Password: pw},
	}, nil
}
