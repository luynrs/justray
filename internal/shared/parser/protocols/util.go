package protocols

import (
	"cmp"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/luynrs/justray/internal/shared/domain"
)

func Unbase64(s string) ([]byte, error) {
	s = strings.Join(strings.Fields(s), "")
	s = strings.NewReplacer("-", "+", "_", "/").Replace(s)
	if pad := len(s) % 4; pad != 0 {
		s += strings.Repeat("=", 4-pad)
	}
	return base64.StdEncoding.DecodeString(s)
}

func id(raw string) string {
	sum := sha1.Sum([]byte(raw))
	return hex.EncodeToString(sum[:])[:8]
}

func NodeID(n domain.Node) string {
	wg := ""
	if n.WireGuard != nil {
		wg = n.WireGuard.PrivateKey + n.WireGuard.PeerPublicKey
	}
	tls := ""
	if n.TLS != nil {
		tls = n.TLS.SNI + strings.Join(n.TLS.ALPN, ",") + n.TLS.Fingerprint
	}
	reality := ""
	if n.Reality != nil {
		reality = n.Reality.PublicKey + n.Reality.ShortID
	}
	shadowTLS := ""
	if n.ShadowTLS != nil {
		shadowTLS = fmt.Sprintf("%d:%s:%s", n.ShadowTLS.Version, n.ShadowTLS.Password, n.ShadowTLS.SNI)
	}
	return id(strings.Join([]string{
		string(n.Protocol), n.Server, strconv.Itoa(n.Port),
		n.Auth.UUID, n.Auth.Password, n.Auth.Username,
		n.Transport.Network, n.Transport.Path, n.Transport.Host, n.Transport.ServiceName,
		tls, reality, wg, shadowTLS,
	}, "|"))
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
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes":
		return true
	}
	return false
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
