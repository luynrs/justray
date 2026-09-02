package subscription

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"

	"github.com/luynrs/justray/internal/daemon/platform/device"
	"github.com/luynrs/justray/internal/shared/version"
)

func deviceHeaders(ctx context.Context) (http.Header, error) {
	h := http.Header{}
	set := func(key, val string) {
		if val != "" {
			h.Set(key, val)
		}
	}
	set("User-Agent", "justray/"+version.String())
	osName, hwid, ver, model := device.Info(ctx)
	set("X-Device-OS", osName)
	set("X-Hwid", hash(hwid))
	set("X-Ver-OS", ver)
	set("X-Device-Model", model)

	if h.Get("X-Hwid") == "" {
		return h, fmt.Errorf("no machine id on %s", osName)
	}
	return h, nil
}

func hash(raw string) string {
	if raw == "" {
		return ""
	}
	sum := sha256.Sum256([]byte("justray:" + raw))
	return hex.EncodeToString(sum[:16])
}
