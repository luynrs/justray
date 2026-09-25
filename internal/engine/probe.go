package engine

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	sbox "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/option"
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
	if ctx.Err() != nil {
		return ctx.Err()
	}
	opts := ProbeConfig(ctx, nodes, s, logPath)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	inst, err := startProbeEngine(ctx, opts)
	if err != nil {
		return err
	}
	defer func() { _ = inst.Close() }()

	sem := make(chan struct{}, min(len(nodes), maxProbeWorkers))
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
			if err != nil {
				ms = 0
			}
			onResult(n.ID, Result{Alive: err == nil, MS: ms})
		})
	}
	wg.Wait()
	return ctx.Err()
}

func delay(ctx context.Context, dialer N.Dialer, url string) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	client := &http.Client{
		Transport: &http.Transport{
			DisableKeepAlives: true,
			DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
				return dialer.DialContext(ctx, N.NetworkTCP, M.ParseSocksaddr(addr))
			},
		},
	}
	defer client.CloseIdleConnections()

	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if errors.Is(err, net.ErrClosed) {
		resp, err = client.Do(req)
	}
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

func startProbeEngine(ctx context.Context, opts *option.Options) (*sbox.Box, error) {
	inst, err := sbox.New(sbox.Options{Options: *opts, Context: Context(ctx)})
	if err == nil {
		if inst.Start() == nil {
			return inst, nil
		}
		_ = inst.Close()
	}

	byTag := make(map[string]option.Outbound, len(opts.Outbounds))
	for _, ob := range opts.Outbounds {
		byTag[ob.Tag] = ob
	}
	opts.Outbounds = slices.DeleteFunc(opts.Outbounds, func(ob option.Outbound) bool {
		if strings.HasSuffix(ob.Tag, "-stls") || ob.Tag == "direct" {
			return false
		}
		obs := []option.Outbound{ob}
		if helper, ok := byTag[ob.Tag+"-stls"]; ok {
			obs = append(obs, helper)
		}
		return !canStart(ctx, option.Options{Route: opts.Route, DNS: opts.DNS, Outbounds: obs})
	})
	kept := make(map[string]bool, len(opts.Outbounds))
	for _, ob := range opts.Outbounds {
		kept[ob.Tag] = true
	}
	opts.Outbounds = slices.DeleteFunc(opts.Outbounds, func(ob option.Outbound) bool {
		if base, ok := strings.CutSuffix(ob.Tag, "-stls"); ok {
			return !kept[base]
		}
		return false
	})
	opts.Endpoints = slices.DeleteFunc(opts.Endpoints, func(ep option.Endpoint) bool {
		return !canStart(ctx, option.Options{Route: opts.Route, DNS: opts.DNS, Endpoints: []option.Endpoint{ep}})
	})

	inst, err = sbox.New(sbox.Options{Options: *opts, Context: Context(ctx)})
	if err != nil {
		return nil, fmt.Errorf("build probe engine: %w", err)
	}
	if err := inst.Start(); err != nil {
		_ = inst.Close()
		return nil, fmt.Errorf("start probe engine: %w", err)
	}
	return inst, nil
}

func canStart(ctx context.Context, testOpts option.Options) bool {
	testOpts.Log = &option.LogOptions{Output: os.DevNull}
	inst, err := sbox.New(sbox.Options{Options: testOpts, Context: Context(ctx)})
	if err != nil {
		return false
	}
	defer func() { _ = inst.Close() }()
	return inst.Start() == nil
}
