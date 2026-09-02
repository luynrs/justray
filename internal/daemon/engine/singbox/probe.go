package singbox

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	sbox "github.com/sagernet/sing-box"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"

	"github.com/luynrs/justray/internal/daemon/engine"
	"github.com/luynrs/justray/internal/shared/domain"
)

func Probe(ctx context.Context, nodes []domain.Node, s domain.Settings, logPath string) (map[string]engine.Result, error) {
	if len(nodes) > maxProbeNodes {
		return nil, fmt.Errorf("too many nodes to probe: %d (maximum %d)", len(nodes), maxProbeNodes)
	}
	opts, err := ProbeConfig(ctx, nodes, s, logPath)
	if err != nil {
		return nil, err
	}
	inst, err := sbox.New(sbox.Options{Options: *opts, Context: Context(ctx)})
	if err != nil {
		return nil, fmt.Errorf("build probe engine: %w", err)
	}
	if err := inst.Start(); err != nil {
		_ = inst.Close()
		return nil, fmt.Errorf("start probe engine: %w", err)
	}
	defer func() { _ = inst.Close() }()

	out := map[string]engine.Result{}
	sem := make(chan struct{}, probeWorkers)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i, n := range nodes {
		dialer, ok := inst.Outbound().Outbound(ProbeTag(i))
		if !ok {
			mu.Lock()
			out[n.ID] = engine.Result{}
			mu.Unlock()
			continue
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return nil, ctx.Err()
		}
		wg.Go(func() {
			defer func() { <-sem }()

			ms, err := delay(ctx, dialer, s.ProbeURL)
			if err != nil {
				forget(n.Server, s)
			}
			mu.Lock()
			out[n.ID] = engine.Result{Alive: err == nil, MS: ms}
			mu.Unlock()
		})
	}
	wg.Wait()
	return out, nil
}

func delay(ctx context.Context, dialer N.Dialer, url string) (int, error) {
	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			if r.URL.Scheme != "https" && r.URL.Scheme != "http" {
				return fmt.Errorf("probe redirect must use http or https")
			}
			if via[len(via)-1].URL.Scheme == "https" && r.URL.Scheme == "http" {
				return fmt.Errorf("probe redirect must not downgrade to http")
			}
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			return nil
		},
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
				return dialer.DialContext(ctx, N.NetworkTCP, M.ParseSocksaddr(addr))
			},
		},
	}
	defer client.CloseIdleConnections()

	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	ms := int(time.Since(start).Milliseconds())
	if err != nil {
		return ms, err
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= 400 {
		return ms, fmt.Errorf("http %d", resp.StatusCode)
	}
	return ms, nil
}
