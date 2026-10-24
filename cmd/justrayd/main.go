package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/sagernet/sing-box/log"

	"github.com/luynrs/justray/internal/daemon/connection"
	"github.com/luynrs/justray/internal/daemon/core"
	"github.com/luynrs/justray/internal/daemon/server"
	"github.com/luynrs/justray/internal/daemon/store"
	"github.com/luynrs/justray/internal/daemon/subscription"
	"github.com/luynrs/justray/internal/engine"
	"github.com/luynrs/justray/internal/ipc"
	"github.com/luynrs/justray/internal/platform/elevate"
	"github.com/luynrs/justray/internal/platform/lock"
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

	output := logWriter{os.Stderr}
	factory := log.NewDefaultFactory(context.Background(), log.Formatter{
		DisableColors: true, FullTimestamp: true, TimestampFormat: "-0700 2006-01-02 15:04:05",
	}, &output, "", nil, false)
	_ = factory.Start()
	log.SetStdLogger(factory.NewLogger("daemon"))
	dir, err := ipc.Dir()
	if err != nil {
		log.Fatal("cannot find config directory: ", err)
	}
	if err := ipc.EnsureDir(dir); err != nil {
		log.Fatal("cannot create config directory: ", err)
	}
	socket := ipc.Socket(dir)

	logFile, err := os.OpenFile(ipc.DaemonLog(dir), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		log.Fatal("cannot open daemon log: ", err)
	}
	defer func() { _ = logFile.Close() }()

	output = logWriter{logFile}
	stderrInfo, stderrErr := os.Stderr.Stat()
	logInfo, logErr := logFile.Stat()
	if stderrErr != nil || logErr != nil || !os.SameFile(stderrInfo, logInfo) {
		output = append(output, os.Stderr)
		if err := debug.SetCrashOutput(logFile, debug.CrashOptions{}); err != nil {
			log.Warn("cannot configure crash output: ", err)
		}
	}

	for {
		ctx, cancel := context.WithCancel(context.Background())

		ln, unlock, err := server.Listen(socket)
		if err != nil {
			cancel()
			if errors.Is(err, server.ErrRunning) {
				log.Info(err)
				return
			}
			log.Fatal("cannot listen: ", err)
		}
		for _, path := range []string{ipc.DaemonLog(dir), ipc.EngineLog(dir)} {
			if err := ipc.ClearLog(path); err != nil {
				log.Warn("cannot clear log: ", err)
			}
		}

		conn := connection.New(ctx, dir, engine.New)
		subs := subscription.New(ctx)
		app, err := core.New(ctx, store.Disk{Dir: dir}, conn, subs)
		if err != nil {
			_ = ln.Close()
			unlock()
			cancel()
			log.Fatal("cannot start: ", err)
		}
		srv := server.New(ctx, app)
		if err := app.Restore(); err != nil {
			if !errors.Is(err, ipc.ErrElevate) {
				log.Fatal("cannot restore state: ", err)
			}
			reservation, err := lock.Open(socket + ".elevation.lock")
			for deadline := time.Now().Add(30 * time.Second); errors.Is(err, lock.ErrLocked) && time.Now().Before(deadline); {
				time.Sleep(ipc.Retry)
				reservation, err = lock.Open(socket + ".elevation.lock")
			}
			if err != nil {
				log.Fatal("cannot reserve elevation: ", err)
			}
			if err := elevate.Restart(dir, reservation); err != nil {
				lock.Close(reservation)
				app.RestoreFailed(err)
			} else {
				_ = ln.Close()
				unlock()
				cancel()
				ready, finish := context.WithTimeout(context.Background(), 30*time.Second)
				for ipc.New(socket).Ping(ready) != nil && ready.Err() == nil {
					time.Sleep(ipc.Retry)
				}
				if err := ready.Err(); err != nil {
					log.Error("elevated daemon did not start: ", err)
				}
				finish()
				lock.Close(reservation)
				return
			}
		}
		log.Info("listening on ", socket, " (version ", version.String(), ")")

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
			log.Info("shutting down on ", s)
		case <-app.RestartRequested():
			restart = true
			log.Info("shutting down for elevation")
		case <-srv.ShutdownRequested():
			log.Info("shutting down on request")
		case serveErr = <-served:
			log.Error("listener failed: ", serveErr)
		}
		signal.Stop(sig)
		cancel()

		cleaned := make(chan struct{})
		go func() {
			for range served {
			}
			app.Shutdown()
			close(cleaned)
		}()
		select {
		case <-cleaned:
		case <-time.After(5 * time.Second):
			log.Fatal("shutdown timed out")
		}
		unlock()

		if serveErr != nil {
			os.Exit(1)
		}
		if !restart {
			return
		}
	}
}

type logWriter []*os.File

func (writer *logWriter) Write(data []byte) (int, error) {
	record := strconv.Quote(strings.TrimSuffix(string(data), "\n"))
	record = record[1:len(record)-1] + "\n"
	var writeErr error
	for _, file := range *writer {
		_, err := file.WriteString(record)
		writeErr = errors.Join(writeErr, err)
	}
	if writeErr != nil {
		return 0, writeErr
	}
	return len(data), nil
}
