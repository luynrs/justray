package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/luynrs/justray/internal/daemon/connection"
	"github.com/luynrs/justray/internal/daemon/core"
	"github.com/luynrs/justray/internal/daemon/server"
	"github.com/luynrs/justray/internal/daemon/store"
	"github.com/luynrs/justray/internal/daemon/subscription"
	"github.com/luynrs/justray/internal/engine"
	"github.com/luynrs/justray/internal/ipc"
	"github.com/luynrs/justray/internal/platform/lock"
	"github.com/luynrs/justray/internal/version"
)

func TestStop(t *testing.T) {
	if os.Getenv("JUSTRAY_TEST_COMMAND") == "stop" {
		rootCmd.SetArgs([]string{"stop"})
		if err := Execute(); err != nil {
			t.Fatal(err)
		}
		return
	}
	if reply := os.Getenv("JUSTRAY_TEST_REPLY"); reply != "" {
		dir, err := ipc.Dir()
		if err != nil {
			t.Fatal(err)
		}
		ln, unlock, err := server.Listen(ipc.Socket(dir))
		if err != nil {
			t.Fatal(err)
		}
		defer unlock()
		defer func() { _ = ln.Close() }()
		if reply == "real" {
			version.Version = "999.0.0"
			logger := log.New(io.Discard, "", 0)
			app, err := core.New(store.Disk{Dir: dir}, connection.New(t.Context(), dir, engine.New, engine.Probe, logger), subscription.New(t.Context(), logger))
			if err != nil {
				t.Fatal(err)
			}
			defer app.Shutdown()
			daemon := server.New(t.Context(), logger, app)
			done := make(chan error, 1)
			go func() { done <- daemon.Serve(ln) }()
			fmt.Println("ready")
			<-daemon.ShutdownRequested()
			daemon.Shutdown()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			return
		}
		fmt.Println("ready")
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.SetDeadline(time.Now().Add(time.Second))
			var req struct {
				Method string
			}
			if json.NewDecoder(conn).Decode(&req) == nil {
				_, _ = io.WriteString(conn, reply+"\n")
				if req.Method == "Shutdown" {
					_ = conn.Close()
					return
				}
			}
			_ = conn.Close()
		}
	}
	for name, reply := range map[string]string{
		"old":          `{"OK":true,"Result":"pong","Error":""}`,
		"incompatible": "real",
	} {
		t.Run(name, func(t *testing.T) {
			base := ""
			if runtime.GOOS != "windows" {
				base = "/tmp"
			}
			dir, err := os.MkdirTemp(base, "jr-")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = os.RemoveAll(dir) }()
			for _, key := range []string{"HOME", "XDG_CONFIG_HOME", "APPDATA"} {
				t.Setenv(key, dir)
			}
			dir, err = ipc.Dir()
			if err != nil {
				t.Fatal(err)
			}
			if err := ipc.EnsureDir(dir); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestStop$")
			cmd.Env = append(os.Environ(), "JUSTRAY_TEST_REPLY="+reply)
			pipe, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			go func() { _ = cmd.Wait(); close(done) }()
			defer func() { _ = cmd.Process.Kill(); <-done }()
			if line, err := bufio.NewReader(pipe).ReadString('\n'); err != nil || line != "ready\n" {
				t.Fatalf("daemon startup: %q, %v", line, err)
			}
			cli := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestStop$")
			cli.Env = append(os.Environ(), "JUSTRAY_TEST_COMMAND=stop")
			out, err := cli.CombinedOutput()
			if err != nil || !strings.Contains(string(out), "Daemon stopped") {
				t.Fatalf("stop failed: %s, %v", out, err)
			}
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("daemon did not exit after stop")
			}
			if err := ipc.New(ipc.Socket(dir)).Ping(ctx); !errors.Is(err, ipc.ErrNoDaemon) {
				t.Fatalf("daemon still responds: %v", err)
			}
		})
	}
}

func TestWaitStopped(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "daemon.sock")
	unlock, err := lock.File(sock + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if unlock != nil {
			unlock()
		}
	})
	if err := waitStopped(context.Background(), sock, 10*time.Millisecond); err == nil {
		t.Fatal("reported stopped while lock is held")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitStopped(ctx, sock, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}

	unlock()
	unlock = nil
	if err := waitStopped(context.Background(), sock, time.Second); err != nil {
		t.Fatal(err)
	}
}
