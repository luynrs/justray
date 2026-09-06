package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/luynrs/justray/internal/daemon/connection"
	"github.com/luynrs/justray/internal/daemon/core"
	"github.com/luynrs/justray/internal/daemon/server"
	"github.com/luynrs/justray/internal/daemon/store"
	"github.com/luynrs/justray/internal/daemon/subscription"
	"github.com/luynrs/justray/internal/engine"
	"github.com/luynrs/justray/internal/ipc"
	"github.com/luynrs/justray/internal/platform/elevate"
	"github.com/luynrs/justray/internal/version"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-v", "--version":
			fmt.Printf("justrayd %s\n", version.String())
			return
		}
	}

	dir, err := ipc.Dir()
	if err != nil {
		die("resolve config dir:", err)
	}
	if err := ipc.EnsureDir(dir); err != nil {
		die("create config dir:", err)
	}
	socket := ipc.Socket(dir)

	logFile, err := os.OpenFile(ipc.DaemonLog(dir), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		die("open log file:", err)
	}
	defer func() { _ = logFile.Close() }()

	var out io.Writer = logFile
	if !sameFile(os.Stderr, logFile) {
		out = io.MultiWriter(logFile, os.Stderr)
	}
	logger := log.New(out, "justrayd: ", log.LstdFlags)

	if err := ipc.ClearLog(ipc.EngineLog(dir)); err != nil {
		logger.Print(err)
	}

	for {
		ctx, cancel := context.WithCancel(context.Background())

		ln, unlock, err := server.Listen(socket)
		if err != nil {
			cancel()
			if strings.Contains(err.Error(), "already listening") {
				logger.Printf("%v, exiting", err)
				return
			}
			logger.Fatal(err)
		}
		logger.Printf("justrayd %s listening on %s", version.String(), socket)

		st := store.Disk{Dir: dir}
		conn := connection.New(ctx, dir, engine.New, engine.Probe, logger)
		subs := subscription.New(ctx, logger)
		app, err := core.New(st, conn, subs)
		if err != nil {
			unlock()
			cancel()
			die(err)
		}
		srv := server.New(ctx, logger, app)
		app.Restore()

		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

		go srv.AutoRefresh()

		served := make(chan error, 1)
		go func() { served <- srv.Serve(ln) }()

		restart := false
		select {
		case s := <-sig:
			logger.Printf("shutting down (%s)", s)
		case <-app.RestartRequested():
			restart = true
			logger.Print("shutting down for elevated restart")
		case <-srv.ShutdownRequested():
			logger.Print("shutting down by request")
		case err := <-served:
			logger.Printf("shutting down (%v)", err)
		}
		signal.Stop(sig)
		cancel()

		cleaned := make(chan struct{})
		go func() {
			srv.Shutdown()
			app.Shutdown()
			close(cleaned)
		}()
		select {
		case <-cleaned:
		case <-time.After(5 * time.Second):
			logger.Print("shutdown timed out, exiting")
		}
		unlock()

		if !restart {
			return
		}
		if err := elevate.Restart(dir); err != nil {
			logger.Printf("elevated restart failed: %v, continuing non-elevated", err)
			continue
		}
		return
	}
}

func sameFile(a, b *os.File) bool {
	ai, err := a.Stat()
	if err != nil {
		return false
	}
	bi, err := b.Stat()
	return err == nil && os.SameFile(ai, bi)
}

func die(v ...any) {
	fmt.Fprintln(os.Stderr, append([]any{"justrayd:"}, v...)...)
	os.Exit(1)
}
