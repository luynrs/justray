package parser

import (
	"bytes"
	"cmp"
	"compress/zlib"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/sagernet/sing-box/option"

	"github.com/luynrs/justray/internal/domain"
)

func parseAmnezia(uri string) (domain.Node, error) {
	const maxConfigSize = 1 << 20
	_, encoded, _ := strings.Cut(uri, "://")
	if len(encoded) > 2*maxConfigSize {
		return domain.Node{}, errors.New("amnezia: encoded configuration exceeds size limit")
	}
	payload, err := decodeBase64(encoded)
	if err != nil {
		return domain.Node{}, errors.New("amnezia: invalid base64 payload")
	}
	if !bytes.HasPrefix(bytes.TrimSpace(payload), []byte("{")) {
		if len(payload) <= 4 {
			return domain.Node{}, errors.New("amnezia: invalid compressed payload")
		}
		reader, err := zlib.NewReader(bytes.NewReader(payload[4:]))
		if err != nil {
			return domain.Node{}, errors.New("amnezia: invalid compressed payload")
		}
		payload, err = io.ReadAll(io.LimitReader(reader, maxConfigSize+1))
		_ = reader.Close()
		if err != nil {
			return domain.Node{}, errors.New("amnezia: invalid compressed payload")
		}
	}
	if len(payload) > maxConfigSize {
		return domain.Node{}, errors.New("amnezia: configuration exceeds 1 MiB limit")
	}
	var profile struct {
		FormatVersion    int    `json:"format_version"`
		Description      string `json:"description"`
		HostName         string `json:"hostName"`
		DefaultContainer string `json:"defaultContainer"`
		Containers       []struct {
			Container string          `json:"container"`
			AWG       json.RawMessage `json:"awg"`
			WireGuard json.RawMessage `json:"wireguard"`
		} `json:"containers"`
	}
	if json.Unmarshal(payload, &profile) != nil {
		return domain.Node{}, errors.New("amnezia: invalid profile JSON")
	}
	if profile.FormatVersion < 0 || profile.FormatVersion > 1 {
		return domain.Node{}, errors.New("amnezia: unsupported format_version")
	}
	type protocolConfig struct {
		Port       flexInt `json:"port"`
		LastConfig string  `json:"last_config"`
	}
	var selected *protocolConfig
	for _, container := range profile.Containers {
		if profile.DefaultContainer != "" && container.Container != profile.DefaultContainer {
			continue
		}
		var raw json.RawMessage
		switch container.Container {
		case "amnezia-awg", "amnezia-awg2":
			raw = container.AWG
		case "amnezia-wireguard":
			raw = container.WireGuard
		default:
			if profile.DefaultContainer != "" {
				return domain.Node{}, errors.New("amnezia: unsupported defaultContainer")
			}
			continue
		}
		var candidate protocolConfig
		if json.Unmarshal(raw, &candidate) != nil {
			return domain.Node{}, errors.New("amnezia: invalid protocol configuration")
		}
		if candidate.LastConfig == "" {
			if profile.DefaultContainer != "" {
				return domain.Node{}, errors.New("amnezia: selected container has no client configuration")
			}
			continue
		}
		if selected != nil {
			return domain.Node{}, errors.New("amnezia: ambiguous WireGuard containers")
		}
		selected = &candidate
	}
	if selected == nil {
		return domain.Node{}, errors.New("amnezia: no ready WireGuard client configuration")
	}
	var client struct {
		HostName     string   `json:"hostName"`
		Port         *flexInt `json:"port"`
		PrivateKey   string   `json:"client_priv_key"`
		PublicKey    string   `json:"server_pub_key"`
		PreSharedKey string   `json:"psk_key"`
		Address      string   `json:"client_ip"`
		MTU          string   `json:"mtu"`
		Keepalive    string   `json:"persistent_keep_alive"`
	}
	if json.Unmarshal([]byte(selected.LastConfig), &client) != nil {
		return domain.Node{}, errors.New("amnezia: invalid last_config client fields")
	}
	var fields map[string]any
	if json.Unmarshal([]byte(selected.LastConfig), &fields) != nil {
		return domain.Node{}, errors.New("amnezia: invalid last_config JSON")
	}
	for name := range fields {
		if amneziaFieldName(name) == "" && !slices.Contains([]string{"j1", "j2", "j3", "itime"}, strings.ToLower(name)) {
			delete(fields, name)
		}
	}
	amnezia, err := parseAmneziaWG(fields)
	if err != nil {
		return domain.Node{}, err
	}
	mtu, err := strconv.ParseUint(cmp.Or(client.MTU, "0"), 10, 32)
	if err != nil {
		return domain.Node{}, errors.New("amnezia: invalid mtu")
	}
	node := domain.Node{
		Protocol: domain.WG,
		Server:   strings.Trim(cmp.Or(client.HostName, profile.HostName), "[]"),
		Port:     int(selected.Port),
		WireGuard: &domain.WireGuard{
			AmneziaWGOptions: amnezia,
			PrivateKey:       client.PrivateKey,
			PeerPublicKey:    client.PublicKey,
			PreSharedKey:     client.PreSharedKey,
			Address:          parseAddresses(client.Address, ""),
			MTU:              uint32(mtu),
		},
	}
	node.Name = cmp.Or(profile.Description, node.Server)
	if client.Port != nil {
		node.Port = int(*client.Port)
	}
	if keepalive := option.AWGRange(client.Keepalive); keepalive != "" {
		node.WireGuard.PersistentKeepaliveInterval = &keepalive
	}
	return node, nil
}

var amneziaFields = map[string]string{
	"jc": "jc", "jmin": "jmin", "jmax": "jmax",
	"s1": "s1", "s2": "s2", "s3": "s3", "s4": "s4",
	"h1": "h1", "h2": "h2", "h3": "h3", "h4": "h4",
	"i1": "i1", "i2": "i2", "i3": "i3", "i4": "i4", "i5": "i5",
	"id": "id", "ip": "ip", "ib": "ib",
	"headerprotectionkey":    "header_protection_key",
	"contentpaddingaddition": "content_padding_addition",
	"rekeyaftertime":         "rekey_after_time", "rekeytimeout": "rekey_timeout",
	"rejectaftertime": "reject_after_time", "keepalivetimeout": "keepalive_timeout",
	"maxhandshakeattempts": "max_handshake_attempts",
	"randomtrailers":       "random_trailers", "disablecookies": "disable_cookies",
}

func amneziaFieldName(name string) string {
	name = strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(name, "_", ""), "-", ""))
	return amneziaFields[strings.TrimPrefix(name, "awg")]
}

func parseAmneziaWG(values map[string]any) (option.AmneziaWGOptions, error) {
	fields := make(map[string]any, len(values))
	for name, value := range values {
		if name == "version" {
			switch fmt.Sprint(value) {
			case "0", "1", "2", "3":
				continue
			default:
				return option.AmneziaWGOptions{}, fmt.Errorf("amneziawg: unsupported version %v", value)
			}
		}
		if value == nil || value == "" {
			continue
		}
		field := amneziaFieldName(name)
		if field == "" {
			return option.AmneziaWGOptions{}, fmt.Errorf("amneziawg: unsupported parameter %q", name)
		}
		if _, exists := fields[field]; exists {
			return option.AmneziaWGOptions{}, fmt.Errorf("amneziawg: duplicate parameter %q", field)
		}
		if text, ok := value.(string); ok {
			switch field {
			case "jc", "jmin", "jmax", "s1", "s2", "s3", "s4":
				parsed, err := strconv.ParseUint(text, 10, 32)
				if err != nil {
					return option.AmneziaWGOptions{}, fmt.Errorf("amneziawg: invalid %s", field)
				}
				value = parsed
			case "random_trailers", "disable_cookies":
				switch strings.ToLower(text) {
				case "on", "off":
					value = strings.EqualFold(text, "on")
				default:
					parsed, err := strconv.ParseBool(text)
					if err != nil {
						return option.AmneziaWGOptions{}, fmt.Errorf("amneziawg: invalid %s", field)
					}
					value = parsed
				}
			}
		}
		fields[field] = value
	}
	data, err := json.Marshal(fields)
	if err != nil {
		return option.AmneziaWGOptions{}, fmt.Errorf("amneziawg: invalid parameters: %w", err)
	}
	var options option.AmneziaWGOptions
	if err := json.Unmarshal(data, &options); err != nil {
		return option.AmneziaWGOptions{}, fmt.Errorf("amneziawg: invalid parameters: %w", err)
	}
	return options, nil
}
