package protocols

// Hysteria v1

import (
	"cmp"

	"github.com/luynrs/justray/internal/domain"
)

func ParseHysteria(uri string) (domain.Node, error) {
	u, host, port, err := parseURL("hysteria", uri)
	if err != nil {
		return domain.Node{}, err
	}

	q := u.Query()
	obfs := cmp.Or(q.Get("obfsParam"), q.Get("obfs_param"), q.Get("obfs-param"))
	if obfs == "" && q.Get("obfs") != "xplus" {
		obfs = q.Get("obfs")
	}
	auth := ""
	if u.User != nil {
		if p, ok := u.User.Password(); ok {
			auth = p
		} else {
			auth = u.User.Username()
		}
	}
	n := domain.Node{
		Name:         cmp.Or(u.Fragment, host),
		Protocol:     domain.HY1,
		Server:       host,
		Port:         port,
		Auth:         domain.Auth{Password: cmp.Or(auth, q.Get("auth"), q.Get("auth_str"), q.Get("password"))},
		TLS:          tlsFrom(q, host),
		ObfsPassword: obfs,
		UpMbps:       cmp.Or(atoi(q.Get("upmbps")), 100),
		DownMbps:     cmp.Or(atoi(q.Get("downmbps")), 100),
	}
	return n, nil
}
