package engine

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	sbox "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"

	"github.com/luynrs/justray/internal/domain"
)

func (engine *box) Probe(ctx context.Context, nodes []domain.Node, settings domain.Settings, onResult func(int, Result)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(nodes) == 0 {
		return nil
	}
	settings, err := settings.Normalize()
	if err != nil {
		return err
	}
	var current adapter.Outbound
	activeKey := ""
	if engine.inst != nil && engine.node.Protocol == domain.WG {
		managed, _ := engine.inst.Outbound().Outbound("proxy")
		current = managed.(*proxyOutbound).current.Load()
		activeKey = engine.node.ConfigKey()
	}
	groups := make(map[string][]int)
	for i, node := range nodes {
		key := strconv.Itoa(i)
		if node.Protocol == domain.WG {
			key = node.ConfigKey()
		}
		groups[key] = append(groups[key], i)
	}
	runtimeCtx, cancel := context.WithCancel(withRegistry(ctx))
	defer cancel()
	var instance *sbox.Box
	if len(groups) != 1 || groups[activeKey] == nil {
		startup := time.AfterFunc(4*time.Second, cancel)
		instance, err = sbox.New(sbox.Options{Options: *probeConfig(settings, engine.logPath), Context: runtimeCtx})
		if instance != nil {
			defer func() { _ = instance.Close() }()
		}
		if err == nil {
			err = instance.Start()
		}
		if !startup.Stop() && err == nil {
			err = context.DeadlineExceeded
		}
		if err == nil {
			err = runtimeCtx.Err()
		}
		if err != nil {
			return err
		}
	}
	servers := make(map[string][][]int)
	for _, indices := range groups {
		node := nodes[indices[0]]
		server := domain.ServerAddress(strings.ToLower(node.Server), node.Port)
		servers[server] = append(servers[server], indices)
	}
	slots := make(chan struct{}, 8)
	var workers sync.WaitGroup
	for _, targets := range servers {
		jobs := make(chan []int, len(targets))
		for _, target := range targets {
			jobs <- target
		}
		close(jobs)
		for range min(2, len(targets)) {
			workers.Go(func() {
				for target := range jobs {
					select {
					case slots <- struct{}{}:
					case <-runtimeCtx.Done():
						return
					}
					node := nodes[target[0]]
					var dialer adapter.Outbound
					if node.Protocol == domain.WG && node.ConfigKey() == activeKey {
						dialer = current
					}
					result, err := probe(runtimeCtx, node, instance, "p"+strconv.Itoa(target[0]), settings, dialer)
					<-slots
					if err == nil {
						for _, index := range target {
							onResult(index, result)
						}
					}
				}
			})
		}
	}
	workers.Wait()
	return runtimeCtx.Err()
}

func probe(ctx context.Context, node domain.Node, instance *sbox.Box, tag string, settings domain.Settings, dialer adapter.Outbound) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	if dialer != nil {
		tag = dialer.Tag()
	}
	requestCtx = adapter.WithContext(requestCtx, &adapter.InboundContext{Outbound: tag})
	var err error
	if dialer == nil {
		var runtime *nodeRuntime
		runtime, err = newNodeRuntime(requestCtx, instance.Router(), instance.LogFactory().NewLogger("probe/"+tag), tag, node)
		if err == nil {
			defer func() { _ = runtime.scope.Close() }()
			for _, stage := range adapter.ListStartStages[1:] {
				if err = runtime.start(stage); err != nil {
					break
				}
			}
			dialer = runtime
		}
	}
	var milliseconds int
	if err == nil {
		milliseconds, err = delay(requestCtx, dialer, settings)
	}
	if ctx.Err() != nil {
		return Result{}, ctx.Err()
	}
	if err == nil {
		return Result{Alive: true, Duration: milliseconds}, nil
	}
	result := Result{Failure: "failed", Error: err.Error()}
	if os.IsTimeout(err) || errors.Is(requestCtx.Err(), context.DeadlineExceeded) {
		result.Failure = "t/o"
	}
	return result, nil
}

func delay(ctx context.Context, dialer N.Dialer, settings domain.Settings) (int, error) {
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport: &http.Transport{
			DisableKeepAlives: true,
			DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
				destination := M.ParseSocksaddr(addr).Unwrap()
				if settings.IPVersion == "ipv4" && destination.IsIPv6() || settings.IPVersion == "ipv6" && destination.IsIPv4() {
					return nil, fmt.Errorf("destination %s is excluded by IP version %s", destination, settings.IPVersion)
				}
				return dialer.DialContext(ctx, N.NetworkTCP, destination)
			},
		},
	}
	defer client.CloseIdleConnections()

	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, settings.ProbeURL, nil)
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
	if resp.StatusCode >= http.StatusBadRequest {
		return ms, fmt.Errorf("http %d", resp.StatusCode)
	}
	return ms, nil
}
