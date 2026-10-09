package server

import (
	"context"
	"errors"
	"log"
	"net"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/luynrs/justray/internal/daemon/core"
	"github.com/luynrs/justray/internal/ipc"
	"github.com/luynrs/justray/internal/platform/elevate"
	"github.com/luynrs/justray/internal/platform/lock"
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

var ErrRunning = errors.New("another justrayd is already listening")

func New(ctx context.Context, logger *log.Logger, app *core.Core) *Server {
	ctx, cancel := context.WithCancel(ctx)
	return &Server{
		log: logger, core: app, ctx: ctx, cancel: cancel,
		sem: make(chan struct{}, 32), watchSem: make(chan struct{}, 64),
		active: map[net.Conn]struct{}{}, stop: make(chan struct{}, 1),
	}
}

func Listen(socket string) (net.Listener, func(), error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	var unlock func()
	var err error
	if elevate.Needed(syscall.EPERM) {
		var release func()
		release, err = lock.File(socket + ".elevation.lock")
		if err == nil {
			unlock, err = lock.File(socket + ".lock")
			release()
		}
	} else {
		unlock, err = lock.File(socket + ".lock")
	}
	for errors.Is(err, lock.ErrLocked) {
		check, finish := context.WithTimeout(ctx, 100*time.Millisecond)
		pingErr := ipc.New(socket).Ping(check)
		finish()
		if pingErr == nil || errors.Is(pingErr, ipc.ErrVersion) {
			return nil, nil, ErrRunning
		}
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
		if !elevate.Needed(syscall.EPERM) {
			unlock, err = lock.File(socket + ".lock")
		}
	}

	if err != nil {
		return nil, nil, err
	}

	if conn, err := net.DialTimeout("unix", socket, time.Second); err == nil {
		_ = conn.Close()
		unlock()
		return nil, nil, ErrRunning
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
	if err := ipc.Chown(socket); err != nil {
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
		s.wg.Go(func() { s.handle(conn) })
		s.mu.Unlock()
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
