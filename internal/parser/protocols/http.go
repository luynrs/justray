package protocols

import (
	"cmp"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/luynrs/justray/internal/domain"
)

func ParseHTTP(uri string) (domain.Node, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return domain.Node{}, fmt.Errorf("http: %w", err)
	}
	host := u.Hostname()
	if host == "" {
		return domain.Node{}, errors.New("http: missing host")
	}
	port := 80
	if strings.EqualFold(u.Scheme, "https") {
		port = 443
	}
	if p := u.Port(); p != "" {
		var err error
		port, err = strconv.Atoi(p)
		if err != nil || !domain.ValidPort(port) {
			return domain.Node{}, fmt.Errorf("http: bad port %q", p)
		}
	}
	user, pw := userPass(u)
	q := u.Query()
	n := domain.Node{
		Name:     cmp.Or(u.Fragment, host),
		Protocol: domain.HTTP,
		Server:   host,
		Port:     port,
		Auth:     domain.Auth{Username: user, Password: pw},
	}
	if strings.EqualFold(u.Scheme, "https") || truthy(q.Get("tls")) || q.Get("security") == "tls" {
		n.TLS = tlsFrom(q, host)
	}
	return n, nil
}
