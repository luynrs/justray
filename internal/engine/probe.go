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
	boxoutbound "github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing/common"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/service"

	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/engine/outbound"
)

func Probe(ctx context.Context, nodes []domain.Node, settings domain.Settings, logPath string, onResult func(int, Result)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(nodes) == 0 {
		return nil
	}
	runtimeCtx, cancel := context.WithCancel(withRegistry(ctx))
	defer cancel()
	startup := time.AfterFunc(4*time.Second, cancel)
	settings, err := settings.Normalize()
	var instance *sbox.Box
	if err == nil {
		instance, err = sbox.New(sbox.Options{Options: *probeConfig(settings, logPath), Context: runtimeCtx})
	}
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
	servers := make(map[string][]int)
	for i, node := range nodes {
		server := net.JoinHostPort(strings.ToLower(node.Server), strconv.Itoa(node.Port))
		servers[server] = append(servers[server], i)
	}
	slots := make(chan struct{}, 8)
	var workers sync.WaitGroup
	for _, indices := range servers {
		jobs := make(chan int, len(indices))
		for _, index := range indices {
			jobs <- index
		}
		close(jobs)
		for range min(2, len(indices)) {
			workers.Go(func() {
				for index := range jobs {
					select {
					case slots <- struct{}{}:
					case <-runtimeCtx.Done():
						return
					}
					result, err := probe(runtimeCtx, nodes[index], instance, "p"+strconv.Itoa(index), settings.ProbeURL)
					<-slots
					if err == nil {
						onResult(index, result)
					}
				}
			})
		}
	}
	workers.Wait()
	return runtimeCtx.Err()
}

func probe(ctx context.Context, node domain.Node, instance *sbox.Box, tag, url string) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	requestCtx = adapter.WithContext(requestCtx, &adapter.InboundContext{Outbound: tag})
	endpoint, outbounds, err := outbound.New(node, tag)
	logger := instance.LogFactory().NewLogger("probe/" + tag)
	var dialer adapter.Outbound
	if err == nil && endpoint != nil {
		dialer, err = endpointReg.Create(requestCtx, instance.Router(), logger, tag, endpoint.Type, endpoint.Options)
	} else if err == nil {
		if len(outbounds) > 1 {
			requestCtx = service.ExtendContext(requestCtx)
			helpers := boxoutbound.NewManager(logger, outboundReg, instance.Endpoint(), "")
			service.MustRegister[adapter.OutboundManager](requestCtx, helpers)
			for _, helper := range outbounds[:len(outbounds)-1] {
				err = helpers.Create(requestCtx, instance.Router(), logger, helper.Tag, helper.Type, helper.Options)
				if err != nil {
					break
				}
			}
		}
		if err == nil {
			options := outbounds[len(outbounds)-1]
			dialer, err = outboundReg.CreateOutbound(requestCtx, instance.Router(), logger, tag, options.Type, options.Options)
		}
	}
	if dialer != nil {
		defer func() { _ = common.Close(dialer) }()
	}
	for _, stage := range adapter.ListStartStages {
		if err == nil {
			err = requestCtx.Err()
		}
		if err == nil {
			err = adapter.LegacyStart(dialer, stage)
		}
	}
	var milliseconds int
	if err == nil {
		milliseconds, err = delay(requestCtx, dialer, url)
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

func delay(ctx context.Context, dialer N.Dialer, url string) (int, error) {
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
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
	if resp.StatusCode >= http.StatusBadRequest {
		return ms, fmt.Errorf("http %d", resp.StatusCode)
	}
	return ms, nil
}
