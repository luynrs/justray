package outbound

import (
	"cmp"
	"encoding/json"
	"net/netip"
	"strings"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json/badoption"

	"github.com/luynrs/justray/internal/domain"
)

func transport(n domain.Node) *option.V2RayTransportOptions {
	if _, err := netip.ParseAddr(n.Server); err != nil && n.Transport.Host == "" {
		n.Transport.Host = n.Server
	}
	switch n.Transport.Network {
	case "ws":
		ws := option.V2RayWebsocketOptions{Path: n.Transport.Path}
		if n.Transport.Host != "" {
			ws.Headers = badoption.HTTPHeader{"Host": {n.Transport.Host}}
		}
		return &option.V2RayTransportOptions{Type: C.V2RayTransportTypeWebsocket, WebsocketOptions: ws}
	case "grpc":
		return &option.V2RayTransportOptions{
			Type:        C.V2RayTransportTypeGRPC,
			GRPCOptions: option.V2RayGRPCOptions{ServiceName: n.Transport.ServiceName},
		}
	case "http":
		h := option.V2RayHTTPOptions{Path: n.Transport.Path}
		if n.Transport.Host != "" {
			h.Host = badoption.Listable[string]{n.Transport.Host}
		}
		return &option.V2RayTransportOptions{Type: C.V2RayTransportTypeHTTP, HTTPOptions: h}
	case "httpupgrade":
		return &option.V2RayTransportOptions{
			Type: C.V2RayTransportTypeHTTPUpgrade,
			HTTPUpgradeOptions: option.V2RayHTTPUpgradeOptions{
				Path: n.Transport.Path,
				Host: n.Transport.Host,
			},
		}
	case "xhttp", "splithttp":
		return &option.V2RayTransportOptions{
			Type:         C.V2RayTransportTypeXHTTP,
			XHTTPOptions: xhttpOptions(n.Transport),
		}
	}
	return nil
}

type xhttpExtra struct {
	Path                 string            `json:"path"`
	Host                 string            `json:"host"`
	Mode                 string            `json:"mode"`
	NoGRPCHeader         bool              `json:"noGRPCHeader"`
	SessionIDPlacement   string            `json:"sessionIDPlacement"`
	SessionPlacement     string            `json:"sessionPlacement"`
	SessionIDKey         string            `json:"sessionIDKey"`
	SessionKey           string            `json:"sessionKey"`
	SessionIDTable       string            `json:"sessionIDTable"`
	SessionTable         string            `json:"sessionTable"`
	SessionLength        string            `json:"sessionLength"`
	SeqPlacement         string            `json:"seqPlacement"`
	SeqKey               string            `json:"seqKey"`
	UplinkDataPlacement  string            `json:"uplinkDataPlacement"`
	UplinkDataKey        string            `json:"uplinkDataKey"`
	UplinkChunkSize      string            `json:"uplinkChunkSize"`
	UplinkHTTPMethod     string            `json:"uplinkHTTPMethod"`
	XPaddingBytes        string            `json:"xPaddingBytes"`
	XPaddingObfsMode     bool              `json:"xPaddingObfsMode"`
	XPaddingKey          string            `json:"xPaddingKey"`
	XPaddingHeader       string            `json:"xPaddingHeader"`
	XPaddingPlacement    string            `json:"xPaddingPlacement"`
	XPaddingMethod       string            `json:"xPaddingMethod"`
	ScMaxEachPostBytes   string            `json:"scMaxEachPostBytes"`
	ScMinPostsIntervalMs string            `json:"scMinPostsIntervalMs"`
	Headers              map[string]string `json:"headers"`

	SessionPlacementSnake     string `json:"session_placement"`
	SessionKeySnake           string `json:"session_key"`
	SessionTableSnake         string `json:"session_table"`
	SessionLengthSnake        string `json:"session_length"`
	SeqPlacementSnake         string `json:"seq_placement"`
	SeqKeySnake               string `json:"seq_key"`
	UplinkDataPlacementSnake  string `json:"uplink_data_placement"`
	UplinkDataKeySnake        string `json:"uplink_data_key"`
	UplinkChunkSizeSnake      string `json:"uplink_chunk_size"`
	UplinkHTTPMethodSnake     string `json:"uplink_http_method"`
	XPaddingBytesSnake        string `json:"x_padding_bytes"`
	XPaddingObfsModeSnake     bool   `json:"x_padding_obfs_mode"`
	XPaddingKeySnake          string `json:"x_padding_key"`
	XPaddingHeaderSnake       string `json:"x_padding_header"`
	XPaddingPlacementSnake    string `json:"x_padding_placement"`
	XPaddingMethodSnake       string `json:"x_padding_method"`
	ScMaxEachPostBytesSnake   string `json:"sc_max_each_post_bytes"`
	ScMinPostsIntervalMsSnake string `json:"sc_min_posts_interval_ms"`
}

func xhttpOptions(t domain.Transport) option.V2RayXHTTPOptions {
	opts := option.V2RayXHTTPOptions{Path: t.Path, Host: t.Host, Mode: t.Mode}
	if t.Extra != "" {
		var e xhttpExtra
		if json.Unmarshal([]byte(t.Extra), &e) == nil {
			opts.Path = cmp.Or(opts.Path, e.Path)
			opts.Host = cmp.Or(opts.Host, e.Host)
			opts.Mode = cmp.Or(opts.Mode, e.Mode)
			opts.NoGRPCHeader = e.NoGRPCHeader
			opts.SessionPlacement = cmp.Or(e.SessionIDPlacement, e.SessionPlacement, e.SessionPlacementSnake)
			opts.SessionKey = cmp.Or(e.SessionIDKey, e.SessionKey, e.SessionKeySnake)
			opts.SessionTable = cmp.Or(e.SessionIDTable, e.SessionTable, e.SessionTableSnake)
			opts.SessionLength = cmp.Or(e.SessionLength, e.SessionLengthSnake)
			opts.SeqPlacement = cmp.Or(e.SeqPlacement, e.SeqPlacementSnake)
			opts.SeqKey = cmp.Or(e.SeqKey, e.SeqKeySnake)
			opts.UplinkDataPlacement = cmp.Or(e.UplinkDataPlacement, e.UplinkDataPlacementSnake)
			opts.UplinkDataKey = cmp.Or(e.UplinkDataKey, e.UplinkDataKeySnake)
			opts.UplinkChunkSize = cmp.Or(e.UplinkChunkSize, e.UplinkChunkSizeSnake)
			opts.UplinkHTTPMethod = cmp.Or(e.UplinkHTTPMethod, e.UplinkHTTPMethodSnake)
			if pad := cmp.Or(e.XPaddingBytes, e.XPaddingBytesSnake); pad != "" && pad != "0-0" && pad != "0" {
				opts.XPaddingBytes = pad
			}
			opts.XPaddingObfsMode = e.XPaddingObfsMode || e.XPaddingObfsModeSnake
			opts.XPaddingKey = cmp.Or(e.XPaddingKey, e.XPaddingKeySnake)
			opts.XPaddingHeader = cmp.Or(e.XPaddingHeader, e.XPaddingHeaderSnake)
			opts.XPaddingPlacement = cmp.Or(e.XPaddingPlacement, e.XPaddingPlacementSnake)
			opts.XPaddingMethod = cmp.Or(e.XPaddingMethod, e.XPaddingMethodSnake)
			opts.ScMaxEachPostBytes = cmp.Or(e.ScMaxEachPostBytes, e.ScMaxEachPostBytesSnake)
			opts.ScMinPostsIntervalMs = cmp.Or(e.ScMinPostsIntervalMs, e.ScMinPostsIntervalMsSnake)
			if len(e.Headers) > 0 {
				opts.Headers = make(badoption.HTTPHeader, len(e.Headers))
				for k, v := range e.Headers {
					opts.Headers[k] = badoption.Listable[string]{v}
				}
			}
		}
	}
	if strings.EqualFold(opts.UplinkHTTPMethod, "GET") && (opts.Mode == "" || opts.Mode == "auto") {
		opts.Mode = "packet-up"
	}
	opts.Mode = cmp.Or(opts.Mode, "auto")
	return opts
}
