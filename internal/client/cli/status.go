package cli

import "github.com/spf13/cobra"

var statusCmd = &cobra.Command{
	Use:     "status",
	Short:   "Show status",
	GroupID: cmdGroup,
	Args:    cobra.NoArgs,
}

func (a *app) status(cmd *cobra.Command, args []string) error {
	st, err := a.client.Status()
	if err != nil {
		return err
	}
	stateHeadline(st)

	if st.Connected {
		a.nodeDetails(st)
		return nil
	}

	ref, err := a.client.Active()
	if err != nil || ref.NodeID == "" {
		return nil
	}
	n, err := a.resolveNode(ref.NodeID, ref.SubscriptionID)
	if err != nil || n.ID == "" {
		return nil
	}
	last := [][2]string{{"Last node", a.nodeName(n.Name, n.ID)}}
	fields(append(last, a.nodeFields(n)...)...)
	return nil
}
