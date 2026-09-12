package cli

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/luynrs/justray/internal/ipc"
	"github.com/luynrs/justray/internal/platform/lock"
)

var stopCmd = &cobra.Command{
	Use:     "stop",
	Short:   "Stop daemon",
	GroupID: cmdGroup,
	Args:    cobra.NoArgs,
}

func (a *app) stop(cmd *cobra.Command, args []string) error {
	dir, err := ipc.Dir()
	if err != nil {
		return err
	}
	socket := ipc.Socket(dir)
	ctx := cmd.Context()
	c := ipc.NewClient(socket).WithContext(ctx)
	if c.Ping() != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := waitStopped(ctx, socket, 6*time.Second); err != nil {
			return err
		}
		done("Daemon is not running")
		return nil
	}
	stop := spin("Stopping daemon")
	shutdownErr := c.Shutdown()
	err = waitStopped(ctx, socket, 6*time.Second)
	stop()
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return err
		}
		return errors.Join(shutdownErr, err)
	}
	done("Daemon stopped")
	return nil
}

func waitStopped(ctx context.Context, socket string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for delay := 5 * time.Millisecond; ; delay = min(delay*2, 50*time.Millisecond) {
		unlock, err := lock.File(socket + ".lock")
		if err == nil {
			unlock()
			return nil
		}
		if os.IsNotExist(err) {
			return nil
		}
		if !errors.Is(err, lock.ErrLocked) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
}
