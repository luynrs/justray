// Package server is the RPC transport over the unix socket
package server

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/luynrs/justray/internal/daemon/connection"
	"github.com/luynrs/justray/internal/daemon/platform/lock"
	"github.com/luynrs/justray/internal/daemon/platform/owner"
	"github.com/luynrs/justray/internal/daemon/subscription"
)

type Server struct {
	log  *log.Logger
	conn *connection.Service
	subs *subscription.Service

	ctx    context.Context
	cancel context.CancelFunc
	sem    chan struct{}
	wg     sync.WaitGroup
	mu     sync.Mutex
	ln     net.Listener
	active map[net.Conn]struct{}
}

func New(logger *log.Logger, conn *connection.Service, subs *subscription.Service) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{
		log: logger, conn: conn, subs: subs, ctx: ctx, cancel: cancel,
		sem: make(chan struct{}, 32), active: map[net.Conn]struct{}{},
	}
}

func Listen(socket string) (net.Listener, error) {
	unlock, err := lock.File(socket + ".lock")
	if err != nil {
		return nil, err
	}
	defer unlock()

	if conn, err := net.DialTimeout("unix", socket, time.Second); err == nil {
		_ = conn.Close()
		return nil, fmt.Errorf("another justrayd is already listening on %s", socket)
	}
	_ = os.Remove(socket)

	ln, err := net.Listen("unix", socket)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(socket, 0o600); err != nil {
		_ = ln.Close()
		return nil, err
	}
	if err := owner.File(socket); err != nil {
		_ = ln.Close()
		return nil, err
	}
	return ln, nil
}

func (s *Server) Serve(ln net.Listener) error {
	s.mu.Lock()
	if s.ctx.Err() != nil {
		s.mu.Unlock()
		_ = ln.Close()
		return nil
	}
	s.ln = ln
	s.mu.Unlock()

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

func (s *Server) serve(conn net.Conn) {
	defer func() {
		s.mu.Lock()
		delete(s.active, conn)
		s.mu.Unlock()
		<-s.sem
		s.wg.Done()
	}()
	s.handle(conn)
}
