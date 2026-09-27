package outbound

import (
	"cmp"
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	SJSON "github.com/sagernet/sing/common/json"
	"github.com/sagernet/sing/common/json/badoption"

	"github.com/luynrs/justray/internal/domain"
)

func transport(n domain.Node) (*option.V2RayTransportOptions, error) {
	if n.Transport.Network == "" || n.Transport.Network == "tcp" {
		return nil, nil
	}
	if n.Protocol != domain.VLess && n.Protocol != domain.VMess && n.Protocol != domain.Trojan {
		return nil, fmt.Errorf("%s: unsupported transport %q", n.Protocol, n.Transport.Network)
	}
	if _, err := netip.ParseAddr(n.Server); err != nil && n.Transport.Host == "" {
		n.Transport.Host = n.Server
	}
	switch n.Transport.Network {
	case "ws":
		ws := option.V2RayWebsocketOptions{Path: n.Transport.Path}
		if n.Transport.Host != "" {
			ws.Headers = badoption.HTTPHeader{"Host": {n.Transport.Host}}
		}
		return &option.V2RayTransportOptions{Type: C.V2RayTransportTypeWebsocket, WebsocketOptions: ws}, nil
	case "grpc":
		return &option.V2RayTransportOptions{
			Type:        C.V2RayTransportTypeGRPC,
			GRPCOptions: option.V2RayGRPCOptions{ServiceName: n.Transport.ServiceName},
		}, nil
	case "http":
		h := option.V2RayHTTPOptions{Path: n.Transport.Path}
		if n.Transport.Host != "" {
			h.Host = badoption.Listable[string]{n.Transport.Host}
		}
		return &option.V2RayTransportOptions{Type: C.V2RayTransportTypeHTTP, HTTPOptions: h}, nil
	case "httpupgrade":
		return &option.V2RayTransportOptions{
			Type: C.V2RayTransportTypeHTTPUpgrade,
			HTTPUpgradeOptions: option.V2RayHTTPUpgradeOptions{
				Path: n.Transport.Path,
				Host: n.Transport.Host,
			},
		}, nil
	case "xhttp":
		opts, err := xhttpOptions(n.Transport)
		if err != nil {
			return nil, err
		}
		return &option.V2RayTransportOptions{
			Type:         C.V2RayTransportTypeXHTTP,
			XHTTPOptions: opts,
		}, nil
	}
	return nil, fmt.Errorf("unsupported transport %q", n.Transport.Network)
}

func ValidateTransport(node domain.Node) error {
	_, err := transport(node)
	return err
}

func xhttpOptions(t domain.Transport) (option.V2RayXHTTPOptions, error) {
	var opts option.V2RayXHTTPOptions
	if t.Extra != "" {
		var fields map[string]json.RawMessage
		if json.Unmarshal([]byte(t.Extra), &fields) != nil || fields == nil {
			return option.V2RayXHTTPOptions{}, fmt.Errorf("xhttp: invalid extra")
		}
		for name := range fields {
			switch strings.ToLower(name) {
			case "xmux":
				return option.V2RayXHTTPOptions{}, fmt.Errorf("xhttp: invalid extra")
			case "sc_max_concurrent_posts", "scmaxconcurrentposts", "server_max_header_bytes", "servermaxheaderbytes",
				"no_sse_header", "nosseheader", "sc_max_buffered_posts", "scmaxbufferedposts",
				"sc_stream_up_server_secs", "scstreamupserversecs":
				delete(fields, name)
			}
		}
		for name, aliases := range map[string][]string{
			"no_grpc_header":           {"noGRPCHeader"},
			"session_placement":        {"sessionPlacement", "sessionIDPlacement"},
			"session_key":              {"sessionKey", "sessionIDKey"},
			"session_table":            {"sessionTable", "sessionIDTable"},
			"session_length":           {"sessionLength"},
			"seq_placement":            {"seqPlacement"},
			"seq_key":                  {"seqKey"},
			"uplink_data_placement":    {"uplinkDataPlacement"},
			"uplink_data_key":          {"uplinkDataKey"},
			"uplink_chunk_size":        {"uplinkChunkSize"},
			"uplink_http_method":       {"uplinkHTTPMethod"},
			"x_padding_bytes":          {"xPaddingBytes"},
			"x_padding_obfs_mode":      {"xPaddingObfsMode"},
			"x_padding_key":            {"xPaddingKey"},
			"x_padding_header":         {"xPaddingHeader"},
			"x_padding_placement":      {"xPaddingPlacement"},
			"x_padding_method":         {"xPaddingMethod"},
			"sc_max_each_post_bytes":   {"scMaxEachPostBytes"},
			"sc_min_posts_interval_ms": {"scMinPostsIntervalMs"},
		} {
			for _, alias := range aliases {
				if value, ok := fields[alias]; ok {
					if name == "no_grpc_header" || name == "x_padding_obfs_mode" {
						var current, incoming bool
						if len(fields[name]) > 0 && json.Unmarshal(fields[name], &current) != nil || json.Unmarshal(value, &incoming) != nil {
							return option.V2RayXHTTPOptions{}, fmt.Errorf("xhttp: invalid extra")
						}
						if current || incoming {
							value = json.RawMessage("true")
						}
					} else {
						var text string
						if json.Unmarshal(value, &text) != nil {
							return option.V2RayXHTTPOptions{}, fmt.Errorf("xhttp: invalid extra")
						}
						if text == "" {
							delete(fields, alias)
							continue
						}
					}
					fields[name] = value
					delete(fields, alias)
				}
			}
		}
		data, _ := json.Marshal(fields)
		if SJSON.UnmarshalDisallowUnknownFields(data, &opts) != nil {
			return option.V2RayXHTTPOptions{}, fmt.Errorf("xhttp: invalid extra")
		}
	}
	opts.Path = cmp.Or(t.Path, opts.Path)
	opts.Host = cmp.Or(t.Host, opts.Host)
	opts.Mode = cmp.Or(t.Mode, opts.Mode)
	if opts.XPaddingBytes == "0-0" || opts.XPaddingBytes == "0" {
		opts.XPaddingBytes = ""
	}
	if strings.EqualFold(opts.UplinkHTTPMethod, "GET") && (opts.Mode == "" || opts.Mode == "auto") {
		opts.Mode = "packet-up"
	}
	opts.Mode = cmp.Or(opts.Mode, "auto")
	return opts, nil
}
