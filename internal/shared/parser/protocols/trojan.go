package protocols

import (
	"cmp"
	"fmt"
	"strings"

	"github.com/luynrs/justray/internal/shared/domain"
)

func ParseTrojan(uri string) (domain.Node, error) {
	u, host, port, err := parseURL("trojan", uri)
	if err != nil {
		return domain.Node{}, err
	}
	if u.User == nil || u.User.Username() == "" {
		return domain.Node{}, fmt.Errorf("trojan: missing password")
	}

	q := u.Query()
	n := domain.Node{
		Name:      cmp.Or(u.Fragment, host),
		Protocol:  domain.Trojan,
		Server:    host,
		Port:      port,
		Auth:      domain.Auth{Password: u.User.Username()},
		Transport: transport(q),
	}
	if strings.ToLower(cmp.Or(q.Get("security"), "tls")) != "none" {
		n.TLS = tlsFrom(q, host)
	}
	return n, nil
}
