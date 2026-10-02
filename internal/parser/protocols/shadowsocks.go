package protocols

import (
	"cmp"
	"fmt"
	"net/url"
	"strings"

	"github.com/luynrs/justray/internal/domain"
)

func ParseShadowsocks(uri string) (domain.Node, error) {
	_, rest, _ := strings.Cut(uri, "://")

	rest, remark, _ := strings.Cut(rest, "#")
	rest, query, hasQuery := strings.Cut(rest, "?")
	legacy := !strings.Contains(rest, "@")
	if legacy {
		if decoded, err := Unbase64(rest); err == nil {
			var innerRemark, innerQuery string
			rest, innerRemark, _ = strings.Cut(string(decoded), "#")
			rest, innerQuery, _ = strings.Cut(rest, "?")
			remark = cmp.Or(remark, innerRemark)
			if !hasQuery {
				query = innerQuery
			}
		}
	}
	if unescaped, err := url.PathUnescape(remark); err == nil {
		remark = unescaped
	}
	plugin := parsePluginQuery(query)
	at := strings.LastIndexByte(rest, '@')
	if at < 0 {
		return domain.Node{}, fmt.Errorf("ss: missing host")
	}
	userinfo := rest[:at]
	if !legacy {
		if unescaped, err := url.PathUnescape(userinfo); err == nil {
			userinfo = unescaped
		}
	}
	method, password := splitCreds(userinfo)
	host, port, err := hostPort(strings.TrimSuffix(rest[at+1:], "/")) // SIP002 allows an empty path
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

func parsePluginQuery(query string) string {
	qv, err := url.ParseQuery(query)
	if err == nil && qv.Get("plugin") != "" {
		return qv.Get("plugin")
	}
	for pair := range strings.SplitSeq(query, "&") {
		if k, v, ok := strings.Cut(pair, "="); ok && k == "plugin" {
			if unescaped, err := url.QueryUnescape(v); err == nil {
				return unescaped
			}
			return v
		}
	}
	return ""
}
