package outbound

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/sagernet/sing-box/option"
	SJSON "github.com/sagernet/sing/common/json"

	"github.com/luynrs/justray/internal/domain"
)

func xhttpOptions(transport domain.Transport) (option.V2RayXHTTPOptions, error) {
	fields := make(map[string]json.RawMessage)
	if transport.Extra != "" {
		if err := json.Unmarshal([]byte(transport.Extra), &fields); err != nil {
			return option.V2RayXHTTPOptions{}, fmt.Errorf("xhttp: extra must be a JSON object")
		}
	}
	if extra, ok := fields["extra"]; ok {
		var nested map[string]json.RawMessage
		if err := json.Unmarshal(extra, &nested); err != nil {
			return option.V2RayXHTTPOptions{}, fmt.Errorf("xhttp: extra must be a JSON object")
		}
		if nested == nil {
			nested = make(map[string]json.RawMessage)
		}
		// Xray takes host/path/mode from the outer settings; extra replaces the rest.
		for _, name := range []string{"host", "path", "mode"} {
			delete(nested, name)
			if value, ok := fields[name]; ok {
				nested[name] = value
			}
		}
		fields = nested
	}
	if err := xhttpNames(fields); err != nil {
		return option.V2RayXHTTPOptions{}, err
	}
	for _, name := range []string{"download_settings", "download"} {
		if value, ok := fields[name]; ok && !bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return option.V2RayXHTTPOptions{}, fmt.Errorf("xhttp: downloadSettings is not supported by sing-box-lx")
		}
		delete(fields, name)
	}
	for _, name := range []string{"sc_max_concurrent_posts", "server_max_header_bytes", "no_sse_header", "sc_max_buffered_posts", "sc_stream_up_server_secs"} {
		delete(fields, name)
	}
	for _, name := range []string{"x_padding_bytes", "session_length", "uplink_chunk_size", "sc_max_each_post_bytes", "sc_min_posts_interval_ms"} {
		if value, ok := fields[name]; ok {
			var interval option.XmuxRange
			if err := json.Unmarshal(value, &interval); err != nil {
				return option.V2RayXHTTPOptions{}, fmt.Errorf("xhttp: %s must be an integer or range", name)
			}
			if interval != "" {
				lower, upper, paired := strings.Cut(string(interval), "-")
				if !paired {
					upper = lower
				}
				minimum, err := strconv.ParseInt(strings.TrimSpace(lower), 10, 32)
				maximum, endErr := strconv.ParseInt(strings.TrimSpace(upper), 10, 32)
				if err != nil || endErr != nil || minimum < 0 || maximum < minimum {
					return option.V2RayXHTTPOptions{}, fmt.Errorf("xhttp: invalid %s range", name)
				}
				if maximum == 0 && (name == "x_padding_bytes" || name == "sc_max_each_post_bytes" || name == "sc_min_posts_interval_ms") {
					interval = ""
				} else if minimum == 0 && (name == "x_padding_bytes" || name == "sc_max_each_post_bytes" || name == "session_length") {
					return option.V2RayXHTTPOptions{}, fmt.Errorf("xhttp: %s range must be positive", name)
				}
			}
			fields[name], _ = json.Marshal(interval)
		}
	}
	if value, ok := fields["xmux"]; ok && !bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		var xmux map[string]json.RawMessage
		if err := json.Unmarshal(value, &xmux); err != nil {
			return option.V2RayXHTTPOptions{}, fmt.Errorf("xhttp: xmux must be a JSON object")
		}
		if err := xhttpNames(xmux); err != nil {
			return option.V2RayXHTTPOptions{}, err
		}
		fields["xmux"], _ = json.Marshal(xmux)
	}
	var options option.V2RayXHTTPOptions
	data, _ := json.Marshal(fields)
	if err := SJSON.UnmarshalDisallowUnknownFields(data, &options); err != nil {
		return options, fmt.Errorf("xhttp: %w", err)
	}
	options.Path = cmp.Or(transport.Path, options.Path)
	options.Host = cmp.Or(transport.Host, options.Host)
	options.Mode = cmp.Or(transport.Mode, options.Mode, "auto")
	if strings.Contains(options.Path, "?") {
		return options, fmt.Errorf("xhttp: query parameters in path are not supported by sing-box-lx")
	}
	for name, values := range options.Headers {
		if strings.EqualFold(name, "Host") && len(values) > 0 {
			options.Host = cmp.Or(options.Host, values[0])
			delete(options.Headers, name)
		}
	}
	options.UplinkHTTPMethod = strings.ToUpper(options.UplinkHTTPMethod)
	if options.UplinkHTTPMethod == "GET" && options.Mode == "auto" {
		options.Mode = "packet-up"
	}
	switch options.Mode {
	case "auto", "packet-up", "stream-up", "stream-one":
	default:
		return options, fmt.Errorf("xhttp: mode must be auto, packet-up, stream-up or stream-one")
	}
	if options.Mode != "packet-up" && (options.UplinkHTTPMethod == "GET" || options.UplinkDataPlacement == "header" || options.UplinkDataPlacement == "cookie") {
		return options, fmt.Errorf("xhttp: GET and header/cookie uploads require packet-up mode")
	}
	return options, nil
}

func xhttpNames(fields map[string]json.RawMessage) error {
	aliases := map[string]string{
		"noGRPCHeader": "no_grpc_header", "noSSEHeader": "no_sse_header",
		"sessionPlacement": "session_placement", "sessionIDPlacement": "session_placement",
		"sessionKey": "session_key", "sessionIDKey": "session_key",
		"sessionTable": "session_table", "sessionIDTable": "session_table",
		"sessionLength": "session_length", "sessionIDLength": "session_length",
		"seqPlacement": "seq_placement", "seqKey": "seq_key",
		"uplinkDataPlacement": "uplink_data_placement", "uplinkDataKey": "uplink_data_key",
		"uplinkChunkSize": "uplink_chunk_size", "uplinkHTTPMethod": "uplink_http_method",
		"xPaddingBytes": "x_padding_bytes", "xPaddingObfsMode": "x_padding_obfs_mode",
		"xPaddingKey": "x_padding_key", "xPaddingHeader": "x_padding_header",
		"xPaddingPlacement": "x_padding_placement", "xPaddingMethod": "x_padding_method",
		"scMaxEachPostBytes": "sc_max_each_post_bytes", "scMinPostsIntervalMs": "sc_min_posts_interval_ms",
		"scMaxConcurrentPosts": "sc_max_concurrent_posts", "scMaxBufferedPosts": "sc_max_buffered_posts",
		"scStreamUpServerSecs": "sc_stream_up_server_secs", "serverMaxHeaderBytes": "server_max_header_bytes",
		"downloadSettings": "download_settings", "reuse-settings": "xmux",
		"maxConcurrency": "max_concurrency", "maxConnections": "max_connections",
		"cMaxReuseTimes": "c_max_reuse_times", "hMaxRequestTimes": "h_max_request_times",
		"hMaxReusableSecs": "h_max_reusable_secs", "hKeepAlivePeriod": "h_keep_alive_period",
	}
	for name, value := range fields {
		canonical := cmp.Or(aliases[name], strings.ReplaceAll(name, "-", "_"))
		if canonical == name {
			continue
		}
		if current, ok := fields[canonical]; ok && !bytes.Equal(current, value) {
			return fmt.Errorf("xhttp: conflicting values for %s", canonical)
		}
		fields[canonical] = value
		delete(fields, name)
	}
	return nil
}
