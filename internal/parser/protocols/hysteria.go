package protocols

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
	auth := cmp.Or(userPassword(u), q.Get("auth"), q.Get("auth_str"), q.Get("password"))
	return domain.Node{
		Name:         cmp.Or(u.Fragment, host),
		Protocol:     domain.HY1,
		Server:       host,
		Port:         port,
		Auth:         domain.Auth{Password: auth},
		TLS:          tlsFrom(q, host),
		ObfsPassword: obfs,
		UpMbps:       cmp.Or(atoi(q.Get("upmbps")), 100),
		DownMbps:     cmp.Or(atoi(q.Get("downmbps")), 100),
	}, nil
}
