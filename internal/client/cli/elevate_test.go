package cli

import (
	"errors"
	"testing"
	"time"

	"github.com/luynrs/justray/internal/shared/rpc"
)

func TestAwaitElevate(t *testing.T) {
	elevatePoll = time.Millisecond
	tun := true

	replies := func(steps ...any) func() (rpc.Status, error) {
		i := -1
		return func() (rpc.Status, error) {
			if i++; i >= len(steps) {
				return rpc.Status{}, errors.New("socket closed")
			}
			switch v := steps[i].(type) {
			case rpc.Status:
				return v, nil
			case error:
				return rpc.Status{}, v
			default:
				panic("unknown reply type")
			}
		}
	}

	t.Run("waits out the restart", func(t *testing.T) {
		status := replies(
			errors.New("connection reset"),        // old daemon, still on its way out
			errors.New("connection refused"),      // prompt still open
			rpc.Status{Connected: true, Tun: true}, // restored
		)
		st, err := awaitElevate(status, &tun, time.Second)
		if err != nil || !st.Tun {
			t.Fatalf("got %+v, %v; want the tun session, nil", st, err)
		}
	})

	t.Run("times out", func(t *testing.T) {
		status := func() (rpc.Status, error) { return rpc.Status{}, errors.New("no daemon") }
		if _, err := awaitElevate(status, &tun, 10*time.Millisecond); err == nil {
			t.Fatal("want a timeout error")
		}
	})
}
