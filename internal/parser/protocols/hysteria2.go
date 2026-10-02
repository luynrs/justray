package protocols

import (
	"cmp"
	"net/url"
	"strings"

	"github.com/luynrs/justray/internal/domain"
)

func ParseHysteria2(uri string) (domain.Node, error) {
	u, host, port, err := parseURL("hysteria2", uri)
	if err != nil {
		return domain.Node{}, err
	}
	q := u.Query()
	auth, _ := url.PathUnescape(strings.TrimPrefix(u.User.String(), ":"))
	obfsPw := cmp.Or(q.Get("obfs-password"), q.Get("obfs_password"), q.Get("obfs-param"), q.Get("obfsparam"))
	obfs := q.Get("obfs")
	if obfs == "" && obfsPw != "" {
		obfs = "salamander"
	}
	return domain.Node{
		Name:         cmp.Or(u.Fragment, host),
		Protocol:     domain.HY2,
		Server:       host,
		Port:         port,
		Auth:         domain.Auth{Password: cmp.Or(auth, q.Get("auth"), q.Get("password"))},
		TLS:          tlsFrom(q, host),
		Obfs:         obfs,
		ObfsPassword: obfsPw,
	}, nil
}
