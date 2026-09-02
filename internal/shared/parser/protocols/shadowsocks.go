package protocols

import (
	"cmp"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/luynrs/justray/internal/shared/domain"
)

// SIP002 ss://base64(method:password)@host:port#remark, or the legacy form
func ParseShadowsocks(uri string) (domain.Node, error) {
	rest := strings.TrimPrefix(uri, "ss://")

	rest, remark, _ := strings.Cut(rest, "#")
	if unescaped, err := url.PathUnescape(remark); err == nil {
		remark = unescaped
	}
	rest, query, hasQuery := strings.Cut(rest, "?")
	var plugin string
	if hasQuery {
		qv, _ := url.ParseQuery(query)
		plugin = qv.Get("plugin")
		if err := checkPlugin(plugin); err != nil {
			return domain.Node{}, fmt.Errorf("ss: %w", err)
		}
	}

	var method, password, hp string
	if at := strings.LastIndexByte(rest, '@'); at >= 0 {
		userinfo := rest[:at]
		if unescaped, err := url.PathUnescape(userinfo); err == nil {
			userinfo = unescaped
		}
		method, password = splitCreds(userinfo)
		hp = rest[at+1:]
	} else {
		decoded, err := Unbase64(rest)
		if err != nil {
			return domain.Node{}, errors.New("invalid ss base64")
		}
		full := string(decoded)
		at := strings.LastIndexByte(full, '@')
		if at < 0 {
			return domain.Node{}, fmt.Errorf("ss: missing host")
		}
		method, password, _ = strings.Cut(full[:at], ":")
		hp = full[at+1:]
	}
	if method == "" || password == "" {
		return domain.Node{}, fmt.Errorf("ss: missing method/password")
	}

	host, port, err := hostPort(strings.TrimSuffix(hp, "/")) // SIP002 allows an empty path
	if err != nil {
		return domain.Node{}, fmt.Errorf("ss: %w", err)
	}

	n := domain.Node{
		Name:     cmp.Or(remark, host),
		Protocol: domain.SS,
		Server:   host,
		Port:     port,
		Auth:     domain.Auth{Method: method, Password: password},
	}
	if plugin != "" {
		stls, err := parseShadowTLSPlugin(plugin, host)
		if err != nil {
			return domain.Node{}, fmt.Errorf("ss: %w", err)
		}
		n.ShadowTLS = stls
	}
	return n, nil
}

// SIP002 credentials are b64
func splitCreds(blob string) (method, password string) {
	if strings.Contains(blob, ":") {
		method, password, _ = strings.Cut(blob, ":")
		return method, password
	}
	if decoded, err := Unbase64(blob); err == nil {
		blob = string(decoded)
	}
	method, password, _ = strings.Cut(blob, ":")
	return method, password
}
