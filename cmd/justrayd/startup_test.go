package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/luynrs/justray/internal/ipc"
	"github.com/luynrs/justray/internal/platform/elevate"
)

func TestStartup(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Setenv("TMPDIR", "/tmp")
	}
	home, err := os.MkdirTemp("", "jr-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	bin := filepath.Join(home, "bin")
	build := exec.Command("go", "build", "-ldflags=-s -w", "-o", bin+string(os.PathSeparator), "./cmd/justray", "./cmd/justrayd")
	build.Dir = "../.."
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %s, %v", output, err)
	}
	for _, key := range []string{"HOME", "APPDATA", "XDG_CONFIG_HOME"} {
		t.Setenv(key, home)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	directory, err := ipc.Dir()
	if err != nil {
		t.Fatal(err)
	}
	if err := ipc.EnsureDir(directory); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	if err := os.WriteFile(ipc.Config(directory), fmt.Appendf(nil, `{"connection":{"port":%d}}`, port), 0o600); err != nil {
		t.Fatal(err)
	}
	client := ipc.New(ipc.Socket(directory))
	run := func(args ...string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		defer cancel()
		if output, err := exec.CommandContext(ctx, filepath.Join(bin, "justray"), args...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %s, %v", args, output, err)
		}
	}
	t.Cleanup(func() { _ = client.Shutdown(context.Background()) })
	start := func() <-chan struct{} {
		t.Helper()
		cmd := exec.Command(filepath.Join(bin, "justrayd"))
		cmd.Stderr = t.Output()
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		t.Cleanup(func() { _ = cmd.Process.Kill(); <-done })
		return done
	}
	ready := func(connected bool) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		for {
			snapshot, err := client.Snapshot(ctx)
			if err == nil {
				if snapshot.Status.Connected != connected {
					t.Fatalf("connection: %+v", snapshot.Status)
				}
				return
			}
			if ctx.Err() != nil {
				t.Fatal(err)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	stopped := exec.Command(filepath.Join(bin, "justray"), "stop")
	if output, err := stopped.CombinedOutput(); err == nil || stopped.ProcessState.ExitCode() != 2 {
		t.Fatalf("stop without daemon: %s, %v", output, err)
	}
	run("sub", "add", "socks://127.0.0.1:19090#fixture")
	run("up", "fixture", "--proxy")
	run("stop")
	start()
	ready(true)
	run("down")
	run("stop")
	start()
	ready(false)
	run("up", "--proxy")
	run("stop")

	listener, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	start()
	ready(false)
	if output, err := exec.Command(filepath.Join(bin, "justray"), "status").CombinedOutput(); err == nil {
		t.Fatalf("restore failure reported success: %s", output)
	}
	_ = listener.Close()
	run("up", "--proxy")
	run("stop")

	if runtime.GOOS != "linux" || !elevate.Needed(syscall.EPERM) {
		return
	}
	data, err := os.ReadFile(ipc.State(directory))
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	state["tun"] = true
	data, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ipc.State(directory), data, 0o600); err != nil {
		t.Fatal(err)
	}
	auth := filepath.Join(home, "auth")
	t.Setenv("TEST_AUTH", auth)
	if err := os.WriteFile(filepath.Join(bin, "pkexec"), []byte("#!/bin/sh\necho request >> \"$TEST_AUTH\"\nwhile [ ! -f \"$TEST_AUTH.release\" ]; do sleep 0.02; done\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.WriteFile(auth+".release", nil, 0o600) })
	start()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(auth); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("daemon did not request elevation")
		}
		time.Sleep(20 * time.Millisecond)
	}
	statusCtx, statusCancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer statusCancel()
	statusResult := make(chan string, 1)
	go func() {
		output, err := exec.CommandContext(statusCtx, filepath.Join(bin, "justray"), "status").CombinedOutput()
		statusResult <- fmt.Sprintf("%s: %v", output, err)
	}()
	duplicate := start()
	select {
	case <-duplicate:
		t.Fatal("duplicate exited before elevation completed")
	case <-time.After(700 * time.Millisecond):
	}
	select {
	case result := <-statusResult:
		t.Fatalf("client did not wait for authorization: %s", result)
	default:
	}
	if err := os.WriteFile(auth+".release", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if result := <-statusResult; !strings.Contains(result, "could not grant permissions") {
		t.Fatalf("waiting client: %s", result)
	}
	ready(false)
	select {
	case <-duplicate:
	case <-time.After(10 * time.Second):
		t.Fatal("duplicate did not exit")
	}
	run("up", "--proxy")
	data, err = os.ReadFile(auth)
	if err != nil || string(data) != "request\n" {
		t.Fatalf("elevation requests: %q, %v", data, err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		output, err := exec.CommandContext(ctx, filepath.Join(bin, "justray"), "up", "--tun").CombinedOutput()
		cancel()
		if err == nil || !strings.Contains(string(output), "could not grant permissions") {
			t.Fatalf("runtime elevation failure: %s, %v", output, err)
		}
		ready(true)
		data, err = os.ReadFile(auth)
		if err != nil || string(data) != strings.Repeat("request\n", attempt+2) {
			t.Fatalf("repeated elevation: %q, %v", data, err)
		}
	}
	run("stop")
	start()
	ready(true)
	run("status")
	run("stop")
}
