package parser

import (
	"cmp"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/luynrs/justray/internal/domain"
)

func decodeBase64(encoded string) ([]byte, error) {
	if strings.Contains(encoded, "%") {
		if unescaped, err := url.PathUnescape(encoded); err == nil {
			encoded = unescaped
		}
	}
	encoded = strings.TrimPrefix(strings.Join(strings.Fields(encoded), ""), "\ufeff")
	encoded = strings.TrimRight(encoded, "=")
	if decoded, err := base64.RawURLEncoding.DecodeString(encoded); err == nil {
		return decoded, nil
	}
	return base64.RawStdEncoding.DecodeString(encoded)
}

func parseHostPort(address string) (string, int, error) {
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return "", 0, errors.New("invalid host or port")
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return "", 0, errors.New("invalid port")
	}
	return host, port, nil
}

func parseCredentials(blob string) (method, password string) {
	if !strings.Contains(blob, ":") {
		if decoded, err := decodeBase64(blob); err == nil && strings.Contains(string(decoded), ":") {
			blob = string(decoded)
		}
	}
	method, password, _ = strings.Cut(blob, ":")
	return method, password
}

func userPassword(parsedURL *url.URL) string {
	if password, found := parsedURL.User.Password(); found {
		return password
	}
	return parsedURL.User.Username()
}

func normalizeCIDRs(list []string) []string {
	var out []string
	for _, a := range list {
		if a == "" {
			continue
		}
		if !strings.Contains(a, "/") {
			if strings.Contains(a, ":") {
				a += "/128"
			} else {
				a += "/32"
			}
		}
		out = append(out, a)
	}
	return out
}

func parseTransport(q url.Values) domain.Transport {
	net := strings.ToLower(cmp.Or(q.Get("type"), q.Get("net"), q.Get("network"), "tcp"))
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
	if net == "xhttp" || net == "splithttp" {
		t.Network, t.Host, t.Mode, t.Extra = "xhttp", q.Get("host"), q.Get("mode"), q.Get("extra")
		if t.Mode == "" {
			switch q.Get("headerType") {
			case "auto", "packet-up", "stream-up", "stream-one":
				t.Mode = q.Get("headerType")
			}
		}
	}
	return t
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

func parseAddresses(v4, v6 string) []string {
	return normalizeCIDRs(append(splitComma(v4), splitComma(v6)...))
}

func cleanFingerprint(fp string, insecure bool) (string, bool) {
	if isCertFingerprint(fp) {
		return "", true
	}
	return fp, insecure
}

// TLS block shared by vless/trojan/anytls links
func parseTLS(q url.Values, host string) *domain.TLS {
	fp, insecure := cleanFingerprint(cmp.Or(q.Get("fp"), q.Get("fingerprint")), insecureFlag(q) || cmp.Or(q.Get("pinSHA256"), q.Get("pinsha256")) != "")
	return &domain.TLS{
		SNI:         cmp.Or(q.Get("sni"), q.Get("peer"), q.Get("server_name"), q.Get("serverName"), host),
		ALPN:        splitComma(q.Get("alpn")),
		Fingerprint: cmp.Or(q.Get("client-fingerprint"), q.Get("clientFingerprint"), fp),
		Insecure:    insecure,
	}
}

func isCertFingerprint(s string) bool {
	clean := strings.ReplaceAll(s, ":", "")
	if len(clean) != 64 && len(clean) != 40 {
		return false
	}
	for _, r := range clean {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return false
		}
	}
	return true
}

type stringOrSlice []string

func (s *stringOrSlice) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '[' {
		return json.Unmarshal(b, (*[]string)(s))
	}
	var str string
	if err := json.Unmarshal(b, &str); err != nil {
		return err
	}
	if str != "" {
		*s = []string{str}
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
	return errors.New("expected a string, boolean or string array")
}
