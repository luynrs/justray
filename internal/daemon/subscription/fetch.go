package subscription

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/luynrs/justray/internal/shared/domain"
	"github.com/luynrs/justray/internal/shared/parser"
)

func (s *Service) fetch(ctx context.Context, rawURL string) ([]domain.Node, string, domain.Traffic, error) {
	var none domain.Traffic
	if err := check(rawURL); err != nil {
		return nil, "", none, err
	}
	if s.device.Get("X-Hwid") == "" {
		return nil, "", none, fmt.Errorf("device id unavailable")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", none, err
	}
	req.Header = s.device.Clone()
	if u, err := url.Parse(rawURL); err == nil {
		req.Header.Set("X-Hwid", hash(s.device.Get("X-Hwid")+u.Hostname()))
	}

	client := http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			if r.URL.Scheme != "https" {
				return fmt.Errorf("subscription redirect must use https")
			}
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			if r.URL.Host != via[0].URL.Host {
				for k := range s.device {
					r.Header.Del(k)
				}
				if hwid := s.device.Get("X-Hwid"); hwid != "" {
					r.Header.Set("X-Hwid", hash(hwid+r.URL.Hostname()))
				}
			}
			return nil
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", none, err
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode != http.StatusOK:
		return nil, "", none, fmt.Errorf("http %d", resp.StatusCode)
	case resp.Header.Get("X-Hwid-Max-Devices-Reached") == "true":
		return nil, "", none, fmt.Errorf("device limit reached")
	case resp.Header.Get("X-Hwid-Not-Supported") == "true":
		return nil, "", none, fmt.Errorf("this subscription requires a device id")
	}

	const maxBody = 10 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, "", none, err
	}
	if len(body) > maxBody {
		return nil, "", none, fmt.Errorf("subscription response exceeded maximum size (10MB)")
	}
	nodes, err := parser.ParseSubscription(body)
	if err != nil {
		return nil, "", none, err
	}
	if err := validateNodes(nodes); err != nil {
		return nil, "", none, err
	}
	return nodes, title(resp.Header), usage(resp.Header), nil
}

// "Subscription-Userinfo: upload=N; download=N; total=N; expire=unixSeconds"
func usage(h http.Header) domain.Traffic {
	var t domain.Traffic
	for field := range strings.SplitSeq(h.Get("Subscription-Userinfo"), ";") {
		key, val, ok := strings.Cut(strings.TrimSpace(field), "=")
		if !ok {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimSpace(val), 10, 64)
		if err != nil {
			continue
		}
		switch strings.TrimSpace(key) {
		case "upload":
			t.UploadBytes = n
		case "download":
			t.DownloadBytes = n
		case "total":
			t.TotalBytes = n
		case "expire":
			if n > 0 { // 0 = never
				t.ExpiresAt = time.Unix(n, 0).UTC()
			}
		}
	}
	return t
}

func title(h http.Header) string {
	if t := h.Get("Profile-Title"); t != "" {
		if b64, ok := strings.CutPrefix(t, "base64:"); ok {
			if decoded, err := base64.StdEncoding.DecodeString(b64); err == nil {
				return string(decoded)
			}
			if decoded, err := base64.RawURLEncoding.DecodeString(b64); err == nil {
				return string(decoded)
			}
		}
		return t
	}
	if cd := h.Get("Content-Disposition"); cd != "" {
		if _, params, err := mime.ParseMediaType(cd); err == nil {
			return params["filename"]
		}
	}
	return ""
}
