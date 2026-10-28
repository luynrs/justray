package outbound

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/transport/v2rayxhttp"
	SJSON "github.com/sagernet/sing/common/json"
	"github.com/sagernet/sing/common/json/badoption"
	M "github.com/sagernet/sing/common/metadata"

	"github.com/luynrs/justray/internal/domain"
)

func transport(n domain.Node) (*option.V2RayTransportOptions, error) {
	if n.Transport.Network == "" || n.Transport.Network == "tcp" {
		if n.Transport.Mode != "" && !strings.EqualFold(n.Transport.Mode, "none") {
			return nil, fmt.Errorf("tcp: unsupported header type: %s", n.Transport.Mode)
		}
		return nil, nil
	}
	if n.Protocol != domain.VLESS && n.Protocol != domain.VMess && n.Protocol != domain.Trojan {
		return nil, fmt.Errorf("%s: unsupported transport: %s", n.Protocol, n.Transport.Network)
	}
	if n.Transport.Network == "xhttp" || n.Transport.Network == "splithttp" {
		opts, err := xhttpOptions(n.Transport)
		if err != nil {
			return nil, err
		}
		return &option.V2RayTransportOptions{Type: C.V2RayTransportTypeXHTTP, XHTTPOptions: opts}, nil
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
		options := option.V2RayHTTPUpgradeOptions{Path: n.Transport.Path, Host: n.Transport.Host}
		if n.Transport.Extra != "" {
			if err := SJSON.Unmarshal([]byte(n.Transport.Extra), &options); err != nil {
				return nil, fmt.Errorf("httpupgrade: %w", err)
			}
		}
		return &option.V2RayTransportOptions{Type: C.V2RayTransportTypeHTTPUpgrade, HTTPUpgradeOptions: options}, nil
	}
	return nil, fmt.Errorf("unsupported transport: %s", n.Transport.Network)
}

func ValidateTransport(node domain.Node) error {
	options, err := transport(node)
	if err != nil {
		return err
	}
	return ValidateTransportOptions(options)
}

func ValidateTransportOptions(options *option.V2RayTransportOptions) error {
	if options == nil || options.Type != C.V2RayTransportTypeXHTTP {
		return nil
	}
	// These two values allocate HTTP metadata; the transport doesn't limit them.
	for _, raw := range []string{options.XHTTPOptions.XPaddingBytes, options.XHTTPOptions.SessionLength} {
		for _, part := range strings.Split(raw, "-") {
			if size, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64); err == nil && size > http.DefaultMaxHeaderBytes {
				return fmt.Errorf("xhttp: metadata exceeds size limit")
			}
		}
	}
	// Construction validates the engine's options without opening a connection.
	client, err := v2rayxhttp.NewClient(context.Background(), nil, M.Socksaddr{}, options.XHTTPOptions, nil)
	if err != nil {
		return fmt.Errorf("xhttp: %w", err)
	}
	return client.Close()
}
