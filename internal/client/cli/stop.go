package cli

import (
	"errors"
	"time"

	"github.com/spf13/cobra"

	"github.com/luynrs/justray/internal/ipc"
)

var stopCmd = &cobra.Command{
	Use:     "stop",
	Short:   "Shut down",
	GroupID: cmdGroup,
	Args:    cobra.NoArgs,
}

func (a *app) stop(cmd *cobra.Command, args []string) error {
	c := a.daemon()
	if c == nil || c.Ping() != nil {
		done("Daemon is not running")
		return nil
	}
	stop := spin("Stopping daemon")
	_ = c.Shutdown()
	err := waitStopped(c, 5*time.Second)
	stop()
	if err != nil {
		return err
	}
	done("Daemon stopped")
	return nil
}

func waitStopped(c *ipc.Client, timeout time.Duration) error {
	for deadline := time.Now().Add(timeout); time.Now().Before(deadline); {
		if c.Ping() != nil {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errors.New("timed out waiting for daemon to stop")
}
