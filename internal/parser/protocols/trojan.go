package protocols

import (
	"cmp"
	"fmt"
	"strings"

	"github.com/luynrs/justray/internal/domain"
)

func ParseTrojan(uri string) (domain.Node, error) {
	u, host, port, err := parseURL("trojan", uri)
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
		return domain.Node{}, fmt.Errorf("trojan: missing password")
	}

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
