package main

import (
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"github.com/luynrs/justray/internal/ipc"
)

func TestLifecycle(t *testing.T) {
	if os.Getenv("JUSTRAY_TEST_DAEMON") == "1" {
		main()
		return
	}
	base := ""
	if runtime.GOOS != "windows" {
		base = "/tmp"
	}
	home, err := os.MkdirTemp(base, "jr-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	for _, key := range []string{"HOME", "APPDATA", "XDG_CONFIG_HOME"} {
		t.Setenv(key, home)
	}
	t.Setenv("JUSTRAY_TEST_DAEMON", "1")
	directory, err := ipc.Dir()
	if err != nil {
		t.Fatal(err)
	}
	client := ipc.NewClient(ipc.Socket(directory))
	start := func() (*exec.Cmd, <-chan struct{}) {
		cmd := exec.Command(os.Args[0], "-test.run=^TestLifecycle$")
		cmd.Stdout, cmd.Stderr = t.Output(), t.Output()
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		t.Cleanup(func() {
			_ = cmd.Process.Kill()
			<-done
		})
		return cmd, done
	}
	wait := func(cmd *exec.Cmd, done <-chan struct{}, ok bool) {
		t.Helper()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("daemon did not exit")
		}
		if cmd.ProcessState.Success() != ok {
			t.Fatalf("daemon exit: %s", cmd.ProcessState)
		}
	}
	for _, crash := range []bool{false, true, false} {
		cmd, done := start()
		end := time.Now().Add(10 * time.Second)
		for client.Ping() != nil {
			select {
			case <-done:
				t.Fatal("daemon exited before ready")
			default:
			}
			if time.Now().After(end) {
				t.Fatal("daemon did not become ready")
			}
			time.Sleep(20 * time.Millisecond)
		}
		if err := os.WriteFile(ipc.EngineLog(directory), []byte("beep boop"), 0o600); err != nil {
			t.Fatal(err)
		}
		duplicate, duplicateDone := start()
		wait(duplicate, duplicateDone, true)
		if contents, err := os.ReadFile(ipc.EngineLog(directory)); err != nil || string(contents) != "beep boop" {
			t.Fatalf("engine log changed: %q, %v", contents, err)
		}
		if crash {
			err = cmd.Process.Kill()
		} else {
			err = client.Shutdown()
		}
		if err != nil {
			t.Fatal(err)
		}
		wait(cmd, done, !crash)
	}
}
