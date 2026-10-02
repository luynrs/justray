package engine

import (
	"context"
	"errors"
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

const maxWorkers = 8

var queue = struct {
	sync.Mutex
	active  int
	servers map[string]int
	changed chan struct{}
}{servers: make(map[string]int), changed: make(chan struct{})}

type Target struct {
	Context context.Context
	Node    domain.Node
}

func Probe(targets []Target, settings domain.Settings, logPath string, onResult func(int, Result, error)) {
	if len(targets) == 0 {
		return
	}
	runtimeCtx, cancel := context.WithCancel(withRegistry(context.WithoutCancel(targets[0].Context)))
	defer cancel()
	startup := time.AfterFunc(4*time.Second, cancel)
	settings, err := settings.Normalize()
	var instance *sbox.Box
	if err == nil {
		instance, err = sbox.New(sbox.Options{Options: *probeConfig(settings, logPath), Context: runtimeCtx})
	}
	if err == nil {
		defer func() { _ = instance.Close() }()
		err = instance.Start()
	}
	if !startup.Stop() && err == nil {
		err = context.DeadlineExceeded
	}
	if err == nil {
		err = runtimeCtx.Err()
	}
	if err != nil {
		for i := range targets {
			onResult(i, Result{}, err)
		}
		return
	}
	var workers sync.WaitGroup
	for i, target := range targets {
		workers.Go(func() {
			ctx := service.ContextWithRegistry(target.Context, service.RegistryFromContext(runtimeCtx))
			result, err := probe(ctx, target.Node, instance, "p"+strconv.Itoa(i), settings.ProbeURL)
			onResult(i, result, err)
		})
	}
	workers.Wait()
}

func probe(ctx context.Context, node domain.Node, instance *sbox.Box, tag, url string) (Result, error) {
	server := net.JoinHostPort(strings.ToLower(node.Server), strconv.Itoa(node.Port))
	queue.Lock()
	for ctx.Err() == nil && (queue.active >= maxWorkers || queue.servers[server] >= 2) {
		changed := queue.changed
		queue.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
		}
		queue.Lock()
	}
	if err := ctx.Err(); err != nil {
		queue.Unlock()
		return Result{}, err
	}
	queue.active++
	queue.servers[server]++
	queue.Unlock()
	defer func() {
		queue.Lock()
		defer queue.Unlock()
		queue.active--
		queue.servers[server]--
		if queue.servers[server] == 0 {
			delete(queue.servers, server)
		}
		close(queue.changed)
		queue.changed = make(chan struct{})
	}()

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
	return ms, nil
}
