package protocols

import (
	"cmp"
	"fmt"
	"net/url"
	"strings"

	"github.com/luynrs/justray/internal/domain"
)

func ParseShadowsocks(uri string) (domain.Node, error) {
	rest := strings.TrimPrefix(uri, "ss://")

	rest, remark, _ := strings.Cut(rest, "#")
	if unescaped, err := url.PathUnescape(remark); err == nil {
		remark = unescaped
	}
	rest, query, hasQuery := strings.Cut(rest, "?")
	var plugin string
	if hasQuery {
		plugin = parsePluginQuery(query)
		if err := checkPlugin(plugin); err != nil {
			return domain.Node{}, fmt.Errorf("ss: %w", err)
		}
	}

	at := strings.LastIndexByte(rest, '@')
	if at < 0 {
		return domain.Node{}, fmt.Errorf("ss: missing host")
	}
	userinfo := rest[:at]
	if unescaped, err := url.PathUnescape(userinfo); err == nil {
		userinfo = unescaped
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
