package subscription

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/luynrs/justray/internal/daemon/store"
	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/parser"
)

func (s *Service) Refresh(ctx context.Context, sub store.Subscription) (store.Subscription, error) {
	if sub.URL == "" {
		return sub, fmt.Errorf("subscription URL or share link is required")
	}
	if parser.IsLink(sub.URL) {
		node, err := parser.ParseURI(sub.URL)
		if err != nil {
			return sub, err
		}
		sub.Nodes, sub.Name, sub.Traffic = []domain.Node{node}, node.Name, domain.Traffic{}
		sub.UpdatedAt = time.Now().UTC()
		return sub, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sub.URL, nil)
	if err != nil || req.URL.Host == "" || (req.URL.Scheme != "https" && req.URL.Scheme != "http") {
		return sub, fmt.Errorf("%q is not a valid URL or share link", sub.URL)
	}
	if s.device.Get("X-Hwid") == "" {
		return sub, fmt.Errorf("device id unavailable")
	}
	req.Header = s.device.Clone()
	req.Header.Set("X-Hwid", hash(s.device.Get("X-Hwid")+req.URL.Hostname()))

	client := http.Client{
		Timeout: 8 * time.Second,
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			if r.URL.Scheme != "https" && r.URL.Scheme != "http" {
				return fmt.Errorf("subscription redirect must use http or https")
			}
			if via[len(via)-1].URL.Scheme == "https" && r.URL.Scheme == "http" {
				return fmt.Errorf("insecure redirect from HTTPS to HTTP")
			}
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			if r.URL.Host != via[0].URL.Host {
				for k := range s.device {
					r.Header.Del(k)
				}
				r.Header.Set("X-Hwid", hash(s.device.Get("X-Hwid")+r.URL.Hostname()))
			}
			return nil
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return sub, err
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode != http.StatusOK:
		return sub, fmt.Errorf("http %d", resp.StatusCode)
	case resp.Header.Get("X-Hwid-Max-Devices-Reached") == "true":
		return sub, fmt.Errorf("device limit reached")
	case resp.Header.Get("X-Hwid-Not-Supported") == "true":
		return sub, fmt.Errorf("subscription requires device ID")
	}

	const maxBody = 10 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return sub, err
	}
	if len(body) > maxBody {
		return sub, fmt.Errorf("subscription response exceeds 10MB limit")
	}
	nodes, warning, err := parser.ParseSubscription(body)
	if err != nil {
		return sub, err
	}
	sub.Nodes, sub.Traffic, sub.Warning = nodes, usage(resp.Header), warning
	sub.UpdatedAt = time.Now().UTC()
	if name := title(resp.Header); name != "" { // change name if it changed on server
		sub.Name = name
	}
	return sub, nil
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
			b64 = strings.TrimRight(strings.TrimSpace(b64), "=")
			if decoded, err := base64.RawStdEncoding.DecodeString(b64); err == nil {
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
