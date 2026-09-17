package protocols

import (
	"cmp"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/luynrs/justray/internal/domain"
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
		plugin = parsePluginQuery(query)
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
		if f, rem, ok := strings.Cut(full, "#"); ok {
			full = f
			if remark == "" {
				if unescaped, err := url.PathUnescape(rem); err == nil {
					remark = unescaped
				} else {
					remark = rem
				}
			}
		}
		if strings.Contains(full, "?") && !hasQuery {
			full, query, hasQuery = strings.Cut(full, "?")
			plugin = parsePluginQuery(query)
			if err := checkPlugin(plugin); err != nil {
				return domain.Node{}, fmt.Errorf("ss: %w", err)
			}
		}
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
