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
		return s.conn.Status(), nil
	case "Active":
		return s.conn.ActiveID()
	case "Subs":
		return s.subs.List()
	case "AddSub":
		return s.subs.Add(s.ctx, a.URL)
	case "RemoveSub":
		return nil, s.removeSub(a.ID)
	case "MoveSub":
		return nil, s.subs.MoveSub(a.ID, a.Dir)
	case "RefreshAll":
		return s.subs.RefreshAll(s.ctx)
	case "Refresh":
		return s.subs.Refresh(s.ctx, a.ID)
	case "Nodes":
		return s.conn.Nodes()
	case "Probe":
		return s.conn.Probe(s.ctx, a.Sub, a.ID)
	case "Connect":
		return s.conn.Connect(a.ID)
	case "Disconnect":
		return s.conn.Disconnect()
	case "SetTun":
		return s.conn.SetTun(a.Tun)
	case "Settings":
		return s.conn.Settings(), nil
	case "SetSettings":
		return s.conn.SetSettings(a.Settings)
	}
	return nil, fmt.Errorf("unknown method %q", req.Method)
}

// removeSub drops the live connection when the deleted sub owned it
func (s *Server) removeSub(id string) error {
	sub, err := s.subs.Remove(id)
	if err != nil {
		return err
	}
	s.conn.ForgetIfRemoved(sub.ID, sub.Nodes)
	return nil
}

func (s *Server) watch(conn net.Conn) {
	_ = conn.SetDeadline(time.Time{}) // stays open

	initial, ch, cancel := s.conn.Watch()
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
