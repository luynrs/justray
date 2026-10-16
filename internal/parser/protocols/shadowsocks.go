package protocols

import (
	"cmp"
	"errors"
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
			rest = string(decoded)
			hostStart := -1
			for index, character := range rest {
				if character == '@' {
					hostStart = index + 1
				}
				if hostStart < 0 || character != '#' && character != '?' {
					continue
				}
				if _, _, err := hostPort(strings.TrimSuffix(rest[hostStart:index], "/")); err != nil {
					hostStart = -1
					continue
				}
				metadata, innerRemark, _ := strings.Cut(rest[index:], "#")
				_, innerQuery, _ := strings.Cut(metadata, "?")
				rest = rest[:index]
				remark = cmp.Or(remark, innerRemark)
				if !hasQuery {
					query = innerQuery
				}
				break
			}
		}
	}
	if unescaped, err := url.PathUnescape(remark); err == nil {
		remark = unescaped
	}
	plugin := parsePluginQuery(query)
	at := strings.LastIndexByte(rest, '@')
	if at < 0 {
		return domain.Node{}, errors.New("ss: missing host")
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
