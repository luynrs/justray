package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/luynrs/justray/internal/daemon/core"
	"github.com/luynrs/justray/internal/daemon/platform/lock"
	"github.com/luynrs/justray/internal/daemon/platform/owner"
)

type Server struct {
	log  *log.Logger
	core *core.Core

	ctx      context.Context
	cancel   context.CancelFunc
	sem      chan struct{}
	watchSem chan struct{}
	wg       sync.WaitGroup
	mu       sync.Mutex
	ln       net.Listener
	active   map[net.Conn]struct{}
	stop     chan struct{}
}

func New(ctx context.Context, logger *log.Logger, app *core.Core) *Server {
	ctx, cancel := context.WithCancel(ctx)
	return &Server{
		log: logger, core: app, ctx: ctx, cancel: cancel,
		sem: make(chan struct{}, 32), watchSem: make(chan struct{}, 64),
		active: map[net.Conn]struct{}{}, stop: make(chan struct{}, 1),
	}
}

func Listen(socket string) (net.Listener, func(), error) {
	unlock, err := lock.File(socket + ".lock")
	if err != nil {
		if errors.Is(err, lock.ErrLocked) {
			return nil, nil, fmt.Errorf("another justrayd is already listening on %s", socket)
		}
		return nil, nil, err
	}

	if conn, err := net.DialTimeout("unix", socket, time.Second); err == nil {
		_ = conn.Close()
		unlock()
		return nil, nil, fmt.Errorf("another justrayd is already listening on %s", socket)
	}
	_ = os.Remove(socket)

	ln, err := net.Listen("unix", socket)
	if err != nil {
		unlock()
		return nil, nil, err
	}
	if err := os.Chmod(socket, 0o600); err != nil {
		_ = ln.Close()
		unlock()
		return nil, nil, err
	}
	if err := owner.File(socket); err != nil {
		_ = ln.Close()
		unlock()
		return nil, nil, err
	}
	return ln, unlock, nil
}

func (s *Server) Serve(ln net.Listener) error {
	s.mu.Lock()
	s.ln = ln
	closed := s.ctx.Err() != nil
	s.mu.Unlock()
	if closed {
		_ = ln.Close()
		return nil
	}

	for {
		conn, err := ln.Accept()
		if err != nil {
			if s.ctx.Err() != nil {
				return nil
			}
			return err
		}
		select {
		case s.sem <- struct{}{}:
		case <-s.ctx.Done():
			_ = conn.Close()
			return nil
		}

		s.mu.Lock()
		if s.ctx.Err() != nil {
			s.mu.Unlock()
			<-s.sem
			_ = conn.Close()
			return nil
		}
		s.active[conn] = struct{}{}
		s.wg.Add(1)
		s.mu.Unlock()
		go s.serve(conn)
	}
}

func (s *Server) Shutdown() {
	s.cancel()
	s.mu.Lock()
	if s.ln != nil {
		_ = s.ln.Close()
	}
	for conn := range s.active {
		_ = conn.Close()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

func (s *Server) ShutdownRequested() <-chan struct{} { return s.stop }

func (s *Server) requestShutdown() {
	select {
	case s.stop <- struct{}{}:
	default:
	}
}

func (s *Server) serve(conn net.Conn) {
	semHeld := true
	defer func() {
		s.mu.Lock()
		delete(s.active, conn)
		s.mu.Unlock()
		if semHeld {
			<-s.sem
		}
		s.wg.Done()
	}()
	s.handle(conn, &semHeld)
}
