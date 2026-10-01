package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/ipc"
	"github.com/luynrs/justray/internal/version"
)

func (s *Server) handle(conn net.Conn) {
	semHeld := true
	defer func() {
		_ = conn.Close()
		s.mu.Lock()
		delete(s.active, conn)
		s.mu.Unlock()
		if semHeld {
			<-s.sem
		}
	}()
	_ = conn.SetDeadline(time.Now().Add(ipc.IdleTimeout))

	var req ipc.Request
	if err := json.NewDecoder(io.LimitReader(conn, 1<<20)).Decode(&req); err != nil { // max req size
		if !errors.Is(err, io.EOF) {
			_ = reply(conn, nil, fmt.Errorf("bad request: %w", err))
		}
		return
	}
	if req.Method == "Ping" {
		_ = reply(conn, "pong", nil)
		return
	}
	if req.Method == "Shutdown" {
		_ = reply(conn, nil, nil)
		select {
		case s.stop <- struct{}{}:
		default:
		}
		return
	}
	if req.Version != version.Version {
		_ = reply(conn, nil, ipc.ErrVersion)
		return
	}
	if req.Method == "Watch" {
		select {
		case s.watchSem <- struct{}{}:
			defer func() { <-s.watchSem }()
		default:
			return
		}
		<-s.sem
		semHeld = false
		s.watch(conn)
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
	_ = conn.SetDeadline(time.Now().Add(ipc.IdleTimeout))
	if reply(conn, result, err) == nil {
		s.core.RequestRestart(err)
	}
}

func (s *Server) dispatch(ctx context.Context, req ipc.Request) (any, error) {
	a := req.Arguments
	switch req.Method {
	case "Snapshot":
		return s.core.Snapshot(), nil
	case "AddSubscription":
		return s.core.AddSubscription(ctx, a.URL)
	case "RemoveSubscription":
		return nil, s.core.RemoveSubscription(ctx, a.SubscriptionID)
	case "RemoveNode":
		return nil, s.core.RemoveNode(ctx, domain.NodeRef{SubscriptionID: a.SubscriptionID, NodeID: a.NodeID})
	case "MoveSubscription":
		return nil, s.core.MoveSubscription(ctx, a.SubscriptionID, a.Direction)
	case "RefreshSubscriptions", "RefreshSubscription":
		var ids []string
		if req.Method == "RefreshSubscription" {
			ids = []string{a.SubscriptionID}
		}
		err := s.core.RefreshSubscriptions(ctx, ids...)
		if err != nil {
			s.log.Printf("refresh failed (%v)", err)
		}
		return nil, err
	case "Probe":
		return nil, s.core.Probe(ctx, a.SubscriptionID, a.NodeID)
	case "Connect":
		return nil, s.core.Connect(ctx, a.NodeID, a.SubscriptionID, a.Tun)
	case "Disconnect":
		return nil, s.core.Disconnect(ctx)
	case "SetTun":
		if a.Tun == nil {
			return nil, errors.New("tun is required")
		}
		return nil, s.core.SetTun(ctx, *a.Tun)
	case "SetSettings":
		return nil, s.core.SetSettings(ctx, a.Settings)
	case "SetAutostart":
		return nil, s.core.SetAutostart(ctx, a.Autostart)
	case "SetCollapsed":
		return nil, s.core.SetCollapsed(ctx, a.SubscriptionID, a.Collapsed)
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

	if err := reply(conn, initial, nil); err != nil {
		return
	}
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-gone:
			return
		case changed := <-ch:
			if err := reply(conn, changed, nil); err != nil {
				return
			}
		}
	}
}

func reply(conn net.Conn, result any, err error) error {
	resp := ipc.Response{Version: version.Version, Success: true}
	if err == nil {
		resp.Result, err = json.Marshal(result)
	}
	if err != nil {
		resp.Success = false
		resp.Error = &ipc.Error{Type: "failure", Message: err.Error()}
		switch {
		case errors.Is(err, ipc.ErrElevate):
			resp.Error.Type = "elevation"
		case errors.Is(err, ipc.ErrVersion):
			resp.Error.Type = "version_mismatch"
		}
	}
	return json.NewEncoder(conn).Encode(resp)
}
