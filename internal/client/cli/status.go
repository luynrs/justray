package cli

import (
	"encoding/json"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:     "status",
	Short:   "Show status",
	GroupID: cmdGroup,
	Args:    cobra.NoArgs,
}

func init() {
	statusCmd.Flags().Bool("json", false, "Output status as JSON")
}

func (a *app) status(cmd *cobra.Command, args []string) error {
	snapshot, err := a.client.Snapshot()
	if err != nil {
		return err
	}
	st := snapshot.Status

	if jsonOut, _ := cmd.Flags().GetBool("json"); jsonOut {
		type statusOut struct {
			Connected bool   `json:"connected"`
			Mode      string `json:"mode,omitempty"`
			Node      string `json:"node,omitempty"`
			Server    string `json:"server,omitempty"`
			Port      int    `json:"port,omitempty"`
			ProxyPort int    `json:"proxy_port,omitempty"`
			Protocol  string `json:"protocol,omitempty"`
			Uptime    int64  `json:"uptime,omitempty"`
			LastNode  string `json:"last_node,omitempty"`
		}
		out := statusOut{Connected: st.Connected}
		if st.Connected {
			n := a.lookupNode(st.NodeRef, snapshot.Nodes)
			out.Mode, out.Node = modeWord(st.Tun), a.clean(st.NodeName)
			out.Server, out.Port, out.Protocol = a.clean(n.Server), n.Port, n.Protocol
			out.Uptime = int64(st.Uptime().Seconds())
			if !st.Tun && st.Port > 0 {
				out.ProxyPort = st.Port
			}
		} else if n := a.lookupNode(snapshot.Selected, snapshot.Nodes); n.ID != "" {
			out.LastNode = a.clean(n.Name)
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}

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
