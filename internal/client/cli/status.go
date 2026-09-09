package cli

import "github.com/spf13/cobra"

var statusCmd = &cobra.Command{
	Use:     "status",
	Short:   "Show status",
	GroupID: cmdGroup,
	Args:    cobra.NoArgs,
}

func (a *app) status(cmd *cobra.Command, args []string) error {
	snapshot, err := a.client.Snapshot()
	if err != nil {
		return err
	}
	st := snapshot.Status
	stateHeadline(st)

	if st.Connected {
		a.nodeDetails(st, snapshot.Nodes)
		return nil
	}

	ref := snapshot.Selected
	if ref.NodeID == "" {
		return nil
	}
	n := a.lookupNode(ref, snapshot.Nodes)
	if n.ID == "" {
		return nil
	}
	last := [][2]string{{"Last node", a.nodeName(n.Name, n.ID)}}
	fields(append(last, a.nodeFields(n)...)...)
	return nil
}
