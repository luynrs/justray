package cli

import "github.com/spf13/cobra"

var downCmd = &cobra.Command{
	Use:     "down",
	Short:   "Disconnect",
	GroupID: cmdGroup,
	Args:    cobra.NoArgs,
}

func (a *app) down(cmd *cobra.Command, args []string) error {
	snapshot, err := a.client.Snapshot()
	if err != nil {
		return err
	}
	st := snapshot.Status
	if !st.Connected {
		done("Already disconnected")
		return nil
	}
	stop := spin("Disconnecting")
	_, err = a.client.Disconnect()
	stop()
	if err != nil {
		return err
	}
	done("Disconnected")
	return nil
}
