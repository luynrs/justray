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
	"github.com/luynrs/justray/internal/domain"
)

func Unbase64(s string) ([]byte, error) {
	if strings.Contains(s, "%") {
		if unescaped, err := url.QueryUnescape(s); err == nil {
			s = unescaped
		}
	}
	s = strings.TrimPrefix(strings.Join(strings.Fields(s), ""), "\ufeff")
	s = strings.TrimRight(s, "=")
	if b, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.RawStdEncoding.DecodeString(s)
}

func NodeID(n domain.Node) string {
	n.ID = ""
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
	net := strings.ToLower(cmp.Or(q.Get("type"), q.Get("net"), q.Get("network"), "tcp"))
	if net == "splithttp" {
		net = "xhttp"
	}
	svc := cmp.Or(q.Get("serviceName"), q.Get("service_name"))
	if net == "grpc" && svc == "" {
		svc = strings.TrimPrefix(q.Get("path"), "/")
	}
	t := domain.Transport{
		Network:     net,
		Path:        q.Get("path"),
		Host:        cmp.Or(q.Get("host"), q.Get("sni")),
		ServiceName: svc,
		Mode:        cmp.Or(q.Get("mode"), q.Get("headerType")),
	}
	if net == "xhttp" {
		t.Extra = q.Get("extra")
	}
	return t
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func splitComma(s string) []string {
	var out []string
	for p := range strings.SplitSeq(s, ",") {
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
	return truthy(q.Get("allowInsecure")) || truthy(q.Get("insecure")) || truthy(q.Get("allow_insecure")) ||
		truthy(q.Get("skip-cert-verify")) || truthy(q.Get("skip_cert_verify")) || truthy(q.Get("skipCertVerify"))
}

// TLS block shared by vless/trojan/anytls links
func tlsFrom(q url.Values, host string) *domain.TLS {
	clientFP := cmp.Or(q.Get("client-fingerprint"), q.Get("clientFingerprint"))
	certFP := cmp.Or(q.Get("pinSHA256"), q.Get("pinsha256"))
	fp := cmp.Or(q.Get("fp"), q.Get("fingerprint"))
	if isCertFingerprint(fp) {
		certFP = fp
		fp = ""
	}
	if clientFP == "" {
		clientFP = fp
	}
	return &domain.TLS{
		SNI:         cmp.Or(q.Get("sni"), q.Get("peer"), q.Get("server_name"), q.Get("serverName"), host),
		ALPN:        splitComma(q.Get("alpn")),
		Fingerprint: clientFP,
		Insecure:    insecureFlag(q) || certFP != "",
	}
}

func isCertFingerprint(s string) bool {
	clean := strings.ReplaceAll(s, ":", "")
	if len(clean) != 64 && len(clean) != 40 {
		return false
	}
	for _, r := range clean {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
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
	n, err := strconv.Atoi(s)
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
	var bVal bool
	if err := json.Unmarshal(b, &bVal); err == nil {
		*f = flexString(strconv.FormatBool(bVal))
		return nil
	}
	var arr []string
	if err := json.Unmarshal(b, &arr); err == nil {
		*f = flexString(strings.Join(arr, ","))
		return nil
	}
	return nil
}
