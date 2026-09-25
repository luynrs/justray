package cli

import (
	"errors"
	"testing"
	"time"

	"github.com/luynrs/justray/internal/ipc"
)

func TestAwaitElevation(t *testing.T) {
	previousPoll := elevatePoll
	elevatePoll = time.Millisecond
	t.Cleanup(func() { elevatePoll = previousPoll })
	attempts := 0
	status := func() (ipc.Status, error) {
		attempts++
		if attempts < 3 {
			return ipc.Status{}, errors.New("daemon restarting")
		}
		return ipc.Status{Connected: true, Tun: true}, nil
	}
	tun := true
	result, err := awaitElevate(t.Context(), status, &tun, time.Second)
	if err != nil || !result.Connected || !result.Tun || attempts != 3 {
		t.Fatalf("elevation result: %+v, attempts=%d, err=%v", result, attempts, err)
	}
}
