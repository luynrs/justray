package outbound_test

import (
	"net/url"
	"testing"

	"github.com/sagernet/sing-box/option"

	"github.com/luynrs/justray/internal/engine/outbound"
	"github.com/luynrs/justray/internal/parser"
)

func builtXHTTP(t *testing.T, extra string) option.V2RayXHTTPOptions {
	t.Helper()
	node, err := parser.ParseURI("vless://uuid@example.com:443?type=xhttp&extra=" + url.QueryEscape(extra))
	if err != nil {
		t.Fatal(err)
	}
	_, outbounds, err := outbound.New(node, "proxy")
	if err != nil {
		t.Fatal(err)
	}
	return outbounds[0].Options.(*option.VLESSOutboundOptions).Transport.XHTTPOptions
}

func TestXHTTPOptions(t *testing.T) {
	got := builtXHTTP(t, `{
		"path": "/xhttp", "sessionIDPlacement": "query", "sessionIDKey": "sid",
		"seqKey": "seq", "uplinkDataKey": "data", "uplinkHTTPMethod": "GET",
		"xPaddingObfsMode": true, "xPaddingKey": "pad", "xPaddingMethod": "tokenish"
	}`)
	if got.Path != "/xhttp" || got.Mode != "packet-up" || got.SessionPlacement != "query" || got.SessionKey != "sid" ||
		got.SeqKey != "seq" || got.UplinkDataKey != "data" || got.UplinkHTTPMethod != "GET" ||
		!got.XPaddingObfsMode || got.XPaddingKey != "pad" || got.XPaddingMethod != "tokenish" {
		t.Fatalf("unexpected options: %+v", got)
	}

	if got := builtXHTTP(t, `{"xPaddingBytes":"0-0"}`); got.XPaddingBytes != "" {
		t.Fatalf("expected empty XPaddingBytes for 0-0, got %q", got.XPaddingBytes)
	}

	got = builtXHTTP(t, `{
		"mode":"packet-up", "session_placement":"header", "session_key":"hdr_sid",
		"x_padding_bytes":"200-400", "no_grpc_header":true, "headers":{"Custom":"val"}
	}`)
	if got.Mode != "packet-up" || got.SessionPlacement != "header" || got.SessionKey != "hdr_sid" ||
		got.XPaddingBytes != "200-400" || !got.NoGRPCHeader || len(got.Headers["Custom"]) == 0 || got.Headers["Custom"][0] != "val" {
		t.Fatalf("unexpected snake options: %+v", got)
	}
}
