package engine

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

	"github.com/luynrs/justray/internal/domain"
)

func Probe(ctx context.Context, nodes []domain.Node, s domain.Settings, logPath string, onResult func(string, Result)) error {
	if len(nodes) > maxProbeNodes {
		return fmt.Errorf("too many nodes to probe: %d (maximum %d)", len(nodes), maxProbeNodes)
	}
	if len(nodes) == 0 {
		return nil
	}
	opts := ProbeConfig(ctx, nodes, s, logPath)
	inst, err := sbox.New(sbox.Options{Options: *opts, Context: Context(ctx)})
	if err != nil {
		return fmt.Errorf("build probe engine: %w", err)
	}
	if err := inst.Start(); err != nil {
		_ = inst.Close()
		return fmt.Errorf("start probe engine: %w", err)
	}
	defer func() { _ = inst.Close() }()

	sem := make(chan struct{}, probeWorkers(len(nodes)))
	var wg sync.WaitGroup
	for i, n := range nodes {
		tag := ProbeTag(i)
		var dialer N.Dialer
		if ob, ok := inst.Outbound().Outbound(tag); ok {
			dialer = ob
		} else if ep, ok := inst.Endpoint().Get(tag); ok {
			dialer = ep
		}
		if dialer == nil {
			onResult(n.ID, Result{})
			continue
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return ctx.Err()
		}
		wg.Go(func() {
			defer func() { <-sem }()

			ms, err := delay(ctx, dialer, s.ProbeURL)
			onResult(n.ID, Result{Alive: err == nil, MS: ms})
		})
	}
	wg.Wait()
	return ctx.Err()
}

func delay(ctx context.Context, dialer N.Dialer, url string) (int, error) {
	client := &http.Client{
		Timeout: 4 * time.Second,
		Transport: &http.Transport{
			DisableKeepAlives: true,
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
