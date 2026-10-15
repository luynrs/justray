package protocols

// TUIC v5

import (
	"cmp"
	"errors"

	"github.com/luynrs/justray/internal/domain"
)

func ParseTUIC(uri string) (domain.Node, error) {
	u, host, port, err := parseURL("tuic", uri)
	if err != nil {
		return domain.Node{}, err
	}
	if u.User == nil || u.User.Username() == "" {
		return domain.Node{}, errors.New("tuic: missing uuid")
	}

	q := u.Query()
	password, _ := u.User.Password()
	tls := tlsFrom(q, host)
	if len(tls.ALPN) == 0 {
		tls.ALPN = []string{"h3"}
	}
	n := domain.Node{
		Name:         cmp.Or(u.Fragment, host),
		Protocol:     domain.TUIC,
		Server:       host,
		Port:         port,
		Auth:         domain.Auth{UUID: u.User.Username(), Password: cmp.Or(password, q.Get("password"))},
		TLS:          tls,
		Congestion:   cmp.Or(q.Get("congestion_control"), q.Get("congestion-control"), q.Get("congestion"), "bbr"),
		UDPRelayMode: cmp.Or(q.Get("udp_relay_mode"), q.Get("udp-relay-mode"), "native"),
	}
	return n, nil
}
