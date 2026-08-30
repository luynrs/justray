package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/luynrs/justray/internal/shared/rpc"
)

func (s *Server) handle(conn net.Conn, semHeld *bool) {
	defer func() { _ = conn.Close() }()
	_ = conn.SetReadDeadline(time.Now().Add(rpc.IdleTimeout))

	var req rpc.Req
	if err := json.NewDecoder(io.LimitReader(conn, 1<<20)).Decode(&req); err != nil { // max req size
		reply(conn, nil, fmt.Errorf("bad request: %w", err))
		return
	}
	if req.Method == "Watch" {
		select {
		case s.watchSem <- struct{}{}:
			defer func() { <-s.watchSem }()
		case <-s.ctx.Done():
			return
		}
		if semHeld != nil && *semHeld {
			<-s.sem
			*semHeld = false
		}
		s.watch(conn)
		return
	}
	if req.Method == "Shutdown" {
		reply(conn, nil, nil)
		s.requestShutdown()
		return
	}
	_ = conn.SetDeadline(time.Time{})
	ctx, cancel := context.WithCancel(s.ctx)
	defer cancel()
	go func() {
		_, _ = conn.Read(make([]byte, 1))
		cancel()
	}()
	result, err := s.dispatch(ctx, req)
	_ = conn.SetDeadline(time.Now().Add(rpc.IdleTimeout))
	reply(conn, result, err)
}

func (s *Server) dispatch(ctx context.Context, req rpc.Req) (any, error) {
	a := req.Args
	switch req.Method {
	case "Ping":
		return "pong", nil
	case "Snapshot":
		return s.core.Snapshot(), nil
	case "AddSub":
		_, err := s.core.AddSubscription(ctx, a.URL)
		return s.mutation(err)
	case "RemoveSub":
		return s.mutation(s.core.RemoveSubscription(a.ID))
	case "MoveSub":
		return s.mutation(s.core.MoveSubscription(a.ID, a.Dir))
	case "RefreshAll":
		_, err := s.core.RefreshSubscriptions(ctx)
		return s.mutation(err)
	case "Refresh":
		_, err := s.core.RefreshSubscription(ctx, a.ID)
		return s.mutation(err)
	case "Probe":
		if err := s.core.Probe(ctx, a.Sub, a.ID); err != nil {
			return nil, err
		}
		return s.core.Snapshot(), nil
	case "Connect":
		_, err := s.core.Connect(ctx, a.ID, a.Sub)
		return s.mutation(err)
	case "Disconnect":
		_, err := s.core.Disconnect(ctx)
		return s.mutation(err)
	case "SetTun":
		_, err := s.core.SetTun(ctx, a.Tun)
		return s.mutation(err)
	case "SetSettings":
		_, err := s.core.SetSettings(ctx, a.Settings)
		return s.mutation(err)
	}
	return nil, fmt.Errorf("unknown method %q", req.Method)
}

func (s *Server) mutation(err error) (any, error) {
	if err != nil {
		return nil, err
	}
	return s.core.Snapshot(), nil
}

func (s *Server) watch(conn net.Conn) {
	_ = conn.SetDeadline(time.Time{}) // stays open

	initial, ch, cancel := s.core.Watch()
	defer cancel()

	gone := make(chan struct{})
	go func() {
		_, _ = conn.Read(make([]byte, 1)) // blocks until the client disconnects
		close(gone)
	}()

	enc := json.NewEncoder(conn)
	if err := enc.Encode(initial); err != nil {
		return
	}
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-gone:
			return
		case changed := <-ch:
			if err := enc.Encode(changed); err != nil {
				return
			}
		}
	}
}

func reply(conn net.Conn, result any, err error) {
	resp := rpc.Resp{OK: true}
	if err == nil {
		var raw []byte
		raw, err = json.Marshal(result)
		resp.Result = raw
	}
	if err != nil {
		resp.OK, resp.Error = false, err.Error()
	}
	_ = json.NewEncoder(conn).Encode(resp)
}
