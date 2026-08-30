package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/luynrs/justray/internal/shared/rpc"
)

func (s *Server) handle(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	_ = conn.SetReadDeadline(time.Now().Add(rpc.IdleTimeout))

	var req rpc.Req
	if err := json.NewDecoder(io.LimitReader(conn, 1<<20)).Decode(&req); err != nil { // max req size
		reply(conn, nil, fmt.Errorf("bad request: %w", err))
		return
	}
	if req.Method == "Watch" {
		s.watch(conn)
		return
	}
	if req.Method == "Shutdown" {
		reply(conn, nil, nil)
		s.requestShutdown()
		return
	}
	_ = conn.SetDeadline(time.Time{})
	result, err := s.dispatch(req)
	_ = conn.SetDeadline(time.Now().Add(rpc.IdleTimeout))
	reply(conn, result, err)
}

func (s *Server) dispatch(req rpc.Req) (any, error) {
	a := req.Args
	switch req.Method {
	case "Ping":
		return "pong", nil
	case "Status":
		return s.core.Status(), nil
	case "Active":
		return s.core.ActiveRef()
	case "Subs":
		return s.core.Subscriptions()
	case "AddSub":
		return s.core.AddSubscription(s.ctx, a.URL)
	case "RemoveSub":
		return nil, s.core.RemoveSubscription(a.ID)
	case "MoveSub":
		return nil, s.core.MoveSubscription(a.ID, a.Dir)
	case "RefreshAll":
		return s.core.RefreshSubscriptions(s.ctx)
	case "Refresh":
		return s.core.RefreshSubscription(s.ctx, a.ID)
	case "Nodes":
		return s.core.Nodes()
	case "Probe":
		return s.core.Probe(s.ctx, a.Sub, a.ID)
	case "Connect":
		return s.core.Connect(a.ID, a.Sub)
	case "Disconnect":
		return s.core.Disconnect()
	case "SetTun":
		return s.core.SetTun(a.Tun)
	case "Settings":
		return s.core.Settings(), nil
	case "SetSettings":
		return s.core.SetSettings(a.Settings)
	}
	return nil, fmt.Errorf("unknown method %q", req.Method)
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
		case st := <-ch:
			if err := enc.Encode(st); err != nil {
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
