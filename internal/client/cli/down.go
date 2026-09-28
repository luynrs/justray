package cli

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/luynrs/justray/internal/ipc"
)

var downCmd = &cobra.Command{
	Use:     "down",
	Short:   "Disconnect",
	GroupID: cmdGroup,
	Args:    cobra.NoArgs,
}

func (a *app) down(cmd *cobra.Command, args []string) error {
	snapshot, err := a.client.Snapshot(cmd.Context())
	if errors.Is(err, ipc.ErrNoDaemon) {
		done("Already disconnected")
		return nil
	}
	if err != nil {
		return err
	}
	st := snapshot.Status
	if !st.Connected {
		done("Already disconnected")
		return nil
	}
	stop := spin("Disconnecting")
	err = a.client.Disconnect(cmd.Context())
	stop()
	if err != nil {
		return err
	}
	done("Disconnected")
	return nil
}
