package cli

import (
	"errors"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/luynrs/justray/internal/ipc"
	"github.com/luynrs/justray/internal/platform/lock"
)

var stopCmd = &cobra.Command{
	Use:     "stop",
	Short:   "Shut down",
	GroupID: cmdGroup,
	Args:    cobra.NoArgs,
}

func (a *app) stop(cmd *cobra.Command, args []string) error {
	dir, err := ipc.Dir()
	if err != nil {
		return err
	}
	socket := ipc.Socket(dir)
	c := ipc.NewClient(socket)
	if c.Ping() != nil {
		if err := waitStopped(socket, 6*time.Second); err != nil {
			return err
		}
		done("Daemon is not running")
		return nil
	}
	stop := spin("Stopping daemon")
	shutdownErr := c.Shutdown()
	err = waitStopped(socket, 6*time.Second)
	stop()
	if err != nil {
		return errors.Join(shutdownErr, err)
	}
	done("Daemon stopped")
	return nil
}

func waitStopped(socket string, timeout time.Duration) error {
	for deadline := time.Now().Add(timeout); time.Now().Before(deadline); {
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
		time.Sleep(50 * time.Millisecond)
	}
	return errors.New("timed out waiting for daemon to stop")
}
