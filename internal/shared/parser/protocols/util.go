package protocols

import (
	"cmp"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/cespare/xxhash/v2"
	"github.com/luynrs/justray/internal/shared/domain"
)

func Unbase64(s string) ([]byte, error) {
	s = strings.Join(strings.Fields(s), "")
	s = strings.TrimRight(s, "=")
	if b, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.RawStdEncoding.DecodeString(s)
}

func NodeID(n domain.Node) string {
	n.ID, n.Name = "", ""
	data, _ := json.Marshal(n)
	return fmt.Sprintf("%016x", xxhash.Sum64(data))
}

func parseURL(proto, uri string) (*url.URL, string, int, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, "", 0, fmt.Errorf("%s: %w", proto, err)
	}
	host, port, err := hostPort(u.Host)
	if err != nil {
		return nil, "", 0, fmt.Errorf("%s: %w", proto, err)
	}
	return u, host, port, nil
}

func hostPort(hp string) (string, int, error) {
	host, p, err := net.SplitHostPort(hp)
	if err != nil {
		return "", 0, err
	}
	port, err := strconv.Atoi(p)
	if err != nil {
		return "", 0, fmt.Errorf("bad port %q", p)
	}
	if !domain.ValidPort(port) {
		return "", 0, fmt.Errorf("port %d out of range", port)
	}
	return host, port, nil
}

func transport(q url.Values) domain.Transport {
	return domain.Transport{
		Network:     strings.ToLower(cmp.Or(q.Get("type"), "tcp")),
		Path:        q.Get("path"),
		Host:        cmp.Or(q.Get("host"), q.Get("sni")),
		ServiceName: q.Get("serviceName"),
	}
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func splitComma(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// covers insecure=1, allowInsecure=true and friends
func truthy(s string) bool {
	v, _ := strconv.ParseBool(strings.TrimSpace(s))
	return v || strings.EqualFold(strings.TrimSpace(s), "yes")
}

func insecureFlag(q url.Values) bool {
	return truthy(q.Get("allowInsecure")) || truthy(q.Get("insecure")) || truthy(q.Get("allow_insecure"))
}

// TLS block shared by vless/trojan/anytls links
func tlsFrom(q url.Values, host string) *domain.TLS {
	return &domain.TLS{
		SNI:         cmp.Or(q.Get("sni"), q.Get("peer"), host),
		ALPN:        splitComma(q.Get("alpn")),
		Fingerprint: q.Get("fp"),
		Insecure:    insecureFlag(q),
	}
}

func checkPlugin(name string) error {
	if base, _, _ := strings.Cut(name, ";"); base != "" && base != "shadow-tls" {
		return fmt.Errorf("unsupported plugin %q", base)
	}
	return nil
}

type flexInt int

func (f *flexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		return nil
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return err
	}
	*f = flexInt(n)
	return nil
}

type flexString string

func (f *flexString) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*f = flexString(s)
		return nil
	}
	var arr []string
	if err := json.Unmarshal(b, &arr); err == nil {
		*f = flexString(strings.Join(arr, ","))
		return nil
	}
	*f = flexString(strings.Trim(string(b), `"`))
	return nil
}
