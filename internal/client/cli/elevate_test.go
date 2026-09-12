package cli

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/luynrs/justray/internal/ipc"
)

func TestAwaitElevate(t *testing.T) {
	elevatePoll = time.Millisecond
	tun := true

	replies := func(steps ...any) func() (ipc.Status, error) {
		i := -1
		return func() (ipc.Status, error) {
			if i++; i >= len(steps) {
				return ipc.Status{}, errors.New("socket closed")
			}
			switch v := steps[i].(type) {
			case ipc.Status:
				return v, nil
			case error:
				return ipc.Status{}, v
			default:
				panic("unknown reply type")
			}
		}
	}

	t.Run("waits out the restart", func(t *testing.T) {
		status := replies(
			errors.New("connection reset"),         // old daemon, still on its way out
			errors.New("connection refused"),       // prompt still open
			ipc.Status{Connected: true, Tun: true}, // restored
		)
		st, err := awaitElevate(t.Context(), status, &tun, time.Second)
		if err != nil || !st.Tun {
			t.Fatalf("got %+v, %v; want the tun session, nil", st, err)
		}
	})

	t.Run("times out", func(t *testing.T) {
		status := func() (ipc.Status, error) { return ipc.Status{}, errors.New("no daemon") }
		if _, err := awaitElevate(t.Context(), status, &tun, 10*time.Millisecond); err == nil {
			t.Fatal("want a timeout error")
		}
	})

	t.Run("cancels", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		status := func() (ipc.Status, error) { return ipc.Status{}, nil }
		if _, err := awaitElevate(ctx, status, &tun, time.Second); !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v, want context.Canceled", err)
		}
	})
}
