package engine

import (
	"context"
	"errors"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	sbox "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/urltest"

	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/engine/outbound"
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
		activeKey = outbound.ConfigKey(engine.node)
	}
	groups := make(map[string][]int)
	for i, node := range nodes {
		key := strconv.Itoa(i)
		if node.Protocol == domain.WG {
			key = outbound.ConfigKey(node)
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
					if node.Protocol == domain.WG && outbound.ConfigKey(node) == activeKey {
						dialer = current
					}
					result, err := probe(runtimeCtx, node, instance, "p"+strconv.Itoa(target[0]), settings.ProbeURL, dialer)
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

func probe(ctx context.Context, node domain.Node, instance *sbox.Box, tag, probeURL string, dialer adapter.Outbound) (Result, error) {
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
			err = runtime.start(adapter.ListStartStages[1:]...)
			dialer = runtime
		}
	}
	var milliseconds uint16
	if err == nil {
		milliseconds, err = urltest.URLTest(requestCtx, probeURL, dialer)
		if errors.Is(err, net.ErrClosed) {
			milliseconds, err = urltest.URLTest(requestCtx, probeURL, dialer)
		}
	}
	if ctx.Err() != nil {
		return Result{}, ctx.Err()
	}
	if err == nil {
		return Result{Alive: true, Duration: int(milliseconds)}, nil
	}
	result := Result{Failure: "failed", Error: err.Error()}
	if os.IsTimeout(err) || errors.Is(requestCtx.Err(), context.DeadlineExceeded) {
		result.Failure = "t/o"
	}
	return result, nil
}
