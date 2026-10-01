package protocols

import (
	"cmp"
	"strings"

	"github.com/luynrs/justray/internal/domain"
)

func ParseTrojan(uri string) (domain.Node, error) {
	u, host, port, err := parseURL("trojan", uri)
	if err != nil {
		return domain.Node{}, err
	}
	q := u.Query()
	pw := cmp.Or(userPassword(u), q.Get("password"), q.Get("auth"))
	n := domain.Node{
		Name:      cmp.Or(u.Fragment, host),
		Protocol:  domain.Trojan,
		Server:    host,
		Port:      port,
		Auth:      domain.Auth{Password: pw},
		Transport: transport(q),
	}
	if strings.ToLower(cmp.Or(q.Get("security"), "tls")) != "none" {
		n.TLS = tlsFrom(q, host)
	}
	return n, nil
}
