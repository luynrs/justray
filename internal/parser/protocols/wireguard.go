package protocols

import (
	"cmp"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/luynrs/justray/internal/domain"
)

// wireguard://private-key@host:port?publickey=...&address=10.0.0.2/32#remark
func ParseWireGuard(uri string) (domain.Node, error) {
	u, host, port, err := parseURL("wireguard", encodeUserinfoSlash(uri))
	if err != nil {
		return domain.Node{}, err
	}
	q := u.Query()
	privateKey := ""
	if u.User != nil {
		privateKey = u.User.Username()
	}
	if privateKey == "" {
		privateKey = queryAny(u, q, "privatekey", "private_key", "private-key", "privkey")
	}
	peerKey := queryAny(u, q, "publickey", "public_key", "public-key", "peer_public_key", "pubkey", "pbk")

	address := addresses(queryAny(u, q, "address", "addresses", "ip"), queryAny(u, q, "ipv6"))
	reservedRaw := queryAny(u, q, "reserved", "reserved_bytes", "reserved-bytes")
	reserved := parseReserved(reservedRaw)
	if reservedRaw != "" && reserved == nil {
		return domain.Node{}, fmt.Errorf("wireguard: invalid reserved bytes")
	}
	var mtu uint64
	if q.Get("mtu") != "" {
		mtu, err = strconv.ParseUint(q.Get("mtu"), 10, 32)
		if err != nil {
			return domain.Node{}, fmt.Errorf("wireguard: invalid mtu")
		}
	}

	return domain.Node{
		Name:     cmp.Or(u.Fragment, host),
		Protocol: domain.WG,
		Server:   host,
		Port:     port,
		WireGuard: &domain.WireGuard{
			PrivateKey:    privateKey,
			PeerPublicKey: peerKey,
			PreSharedKey:  queryAny(u, q, "presharedkey", "preshared_key", "pre-shared-key", "psk"),
			Address:       address,
			Reserved:      reserved,
			MTU:           uint32(mtu),
		},
	}, nil
}

func queryAny(u *url.URL, q url.Values, keys ...string) string {
	for _, k := range keys {
		if v := rawQuery(u, k); v != "" {
			return v
		}
		if v := q.Get(k); v != "" {
			return v
		}
	}
	return ""
}

func parseReserved(s string) []uint8 {
	if s == "" {
		return nil
	}
	s = strings.Trim(s, "[]")
	if strings.Contains(s, ",") {
		parts := strings.Split(s, ",")
		if len(parts) == 3 {
			res := make([]uint8, 3)
			for i, p := range parts {
				n, err := strconv.Atoi(strings.TrimSpace(p))
				if err != nil || n < 0 || n > 255 {
					return nil
				}
				res[i] = uint8(n)
			}
			return res
		}
	}
	if b, err := Unbase64(s); err == nil && len(b) == 3 {
		return b
	}
	return nil
}

func encodeUserinfoSlash(uri string) string {
	start := strings.Index(uri, "://")
	if start < 0 {
		return uri
	}
	start += 3
	end := strings.IndexByte(uri[start:], '@')
	if end < 0 {
		return uri
	}
	end += start
	if strings.ContainsAny(uri[start:end], "?#") {
		return uri
	}
	return uri[:start] + strings.ReplaceAll(uri[start:end], "/", "%2F") + uri[end:]
}

func rawQuery(u *url.URL, key string) string {
	for pair := range strings.SplitSeq(u.RawQuery, "&") {
		name, value, ok := strings.Cut(pair, "=")
		if ok && name == key {
			value, _ = url.PathUnescape(value)
			return value
		}
	}
	return ""
}
