package parser

import (
	"cmp"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/sagernet/sing-box/option"

	"github.com/luynrs/justray/internal/domain"
)

// wireguard://private-key@host:port?publickey=...&address=10.0.0.2/32#remark
func wireguard(uri string) (domain.Node, error) {
	u, err := url.Parse(encodeSlash(uri))
	if err != nil {
		return domain.Node{}, err
	}
	host, port, err := hostPort(u.Host)
	if err != nil {
		return domain.Node{}, fmt.Errorf("wireguard: %w", err)
	}
	q := u.Query()
	raw, _ := url.ParseQuery(strings.ReplaceAll(strings.ReplaceAll(u.RawQuery, "+", "%2B"), ";", "%3B"))
	privateKey := u.User.Username()
	if privateKey == "" {
		privateKey = query(raw, q, "privatekey", "private_key", "private-key", "privkey")
	}
	peerKey := query(raw, q, "publickey", "public_key", "public-key", "peer_public_key", "pubkey", "pbk")

	address := addresses(query(raw, q, "address", "addresses", "ip"), query(raw, q, "ipv6"))
	reservedRaw := query(raw, q, "reserved", "reserved_bytes", "reserved-bytes")
	reserved := parseReserved(reservedRaw)
	if reservedRaw != "" && reserved == nil {
		return domain.Node{}, errors.New("wireguard: invalid reserved bytes")
	}
	var mtu uint64
	if q.Get("mtu") != "" {
		mtu, err = strconv.ParseUint(q.Get("mtu"), 10, 32)
		if err != nil {
			return domain.Node{}, errors.New("wireguard: invalid mtu")
		}
	}
	fields := make(map[string]any)
	for name := range q {
		if strings.EqualFold(name, "ip") {
			continue
		}
		field := amneziaField(name)
		if field == "" {
			if strings.HasPrefix(strings.ToLower(name), "awg") || slices.Contains([]string{"j1", "j2", "j3", "itime"}, strings.ToLower(name)) {
				return domain.Node{}, fmt.Errorf("amneziawg: unsupported parameter: %s", name)
			}
			continue
		}
		if _, exists := fields[field]; exists || len(q[name]) != 1 {
			return domain.Node{}, fmt.Errorf("amneziawg: duplicate parameter: %s", field)
		}
		fields[field] = q.Get(name)
		if field == "header_protection_key" {
			fields[field] = query(raw, q, name)
		}
	}
	amnezia, err := amneziaWG(fields)
	if err != nil {
		return domain.Node{}, err
	}
	var keepalive *option.AWGRange
	if value := option.AWGRange(query(raw, q, "persistent_keepalive_interval", "persistent-keepalive", "keepalive")); value != "" {
		keepalive = &value
	}

	return domain.Node{
		Name:     cmp.Or(u.Fragment, host),
		Protocol: domain.WG,
		Server:   host,
		Port:     port,
		WireGuard: &domain.WireGuard{
			AmneziaWGOptions:            amnezia,
			PersistentKeepaliveInterval: keepalive,
			PrivateKey:                  privateKey,
			PeerPublicKey:               peerKey,
			PreSharedKey:                query(raw, q, "presharedkey", "preshared_key", "pre-shared-key", "psk"),
			Address:                     address,
			Reserved:                    reserved,
			MTU:                         uint32(mtu),
		},
	}, nil
}

func query(raw, q url.Values, keys ...string) string {
	for _, key := range keys {
		if value := cmp.Or(raw.Get(key), q.Get(key)); value != "" {
			return value
		}
	}
	return ""
}

func parseReserved(s string) []uint8 {
	s = strings.Trim(s, "[]")
	if parts := strings.Split(s, ","); len(parts) == 3 {
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
	if b, err := decode(s); err == nil && len(b) == 3 {
		return b
	}
	return nil
}

func encodeSlash(uri string) string {
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
