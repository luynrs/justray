package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
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
	logging "github.com/luynrs/justray/internal/logger"
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

	logger := logging.New(os.Stderr, "justrayd")
	dir, err := ipc.Dir()
	if err != nil {
		logger.Fatalf("find config dir failed (%v)", err)
	}
	if err := ipc.EnsureDir(dir); err != nil {
		logger.Fatalf("create config dir failed (%v)", err)
	}
	socket := ipc.Socket(dir)

	logFile, err := logging.Open(ipc.DaemonLog(dir))
	if err != nil {
		logger.Fatalf("open log failed (%v)", err)
	}
	defer func() { _ = logFile.Close() }()

	logger.SetOutput(logFile)
	if !sameFile(os.Stderr, logFile) {
		logger.SetOutput(io.MultiWriter(logFile, os.Stderr))
		if err := debug.SetCrashOutput(logFile, debug.CrashOptions{}); err != nil {
			logger.Printf("crash log setup failed (%v)", err)
		}
	}

	for {
		ctx, cancel := context.WithCancel(context.Background())

		ln, unlock, err := server.Listen(socket)
		if err != nil {
			cancel()
			if strings.Contains(err.Error(), "already listening") {
				logger.Printf("shutdown (%v)", err)
				return
			}
			logger.Fatalf("listen failed (%v)", err)
		}
		logger.Printf("listening (%s, version %s)", socket, version.String())
		if err := ipc.ClearLog(ipc.EngineLog(dir)); err != nil {
			logger.Printf("clear engine log failed (%v)", err)
		}

		st := store.Disk{Dir: dir}
		conn := connection.New(ctx, dir, engine.New, engine.Probe, logger)
		subs := subscription.New(ctx, logger)
		app, err := core.New(st, conn, subs)
		if err != nil {
			_ = ln.Close()
			unlock()
			cancel()
			logger.Fatalf("startup failed (%v)", err)
		}
		srv := server.New(ctx, logger, app)
		app.Restore()

		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

		go srv.AutoRefresh()

		served := make(chan error, 1)
		go func() {
			served <- srv.Serve(ln)
			close(served)
		}()

		restart := false
		var serveErr error
		select {
		case s := <-sig:
			logger.Printf("shutdown (%s)", s)
		case <-app.RestartRequested():
			restart = true
			logger.Print("shutdown (elevation)")
		case <-srv.ShutdownRequested():
			logger.Print("shutdown (request)")
		case serveErr = <-served:
			logger.Printf("shutdown (%v)", serveErr)
		}
		signal.Stop(sig)
		cancel()

		cleaned := make(chan struct{})
		go func() {
			_ = ln.Close()
			srv.Shutdown()
			<-served
			app.Shutdown()
			close(cleaned)
		}()
		select {
		case <-cleaned:
		case <-time.After(5 * time.Second):
			logger.Fatal("shutdown (timed out)")
		}
		unlock()

		if serveErr != nil {
			logger.Fatalf("serve failed (%v)", serveErr)
		}
		if !restart {
			return
		}
		if err := elevate.Restart(dir); err != nil {
			logger.Printf("elevation failed (%v, continuing unprivileged)", err)
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
