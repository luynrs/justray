package cli

import (
	"encoding/json"
	"fmt"
	"strconv"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"github.com/luynrs/justray/internal/client/tui/style"
	"github.com/luynrs/justray/internal/client/tui/tree"
	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/ipc"
)

var subCmd = &cobra.Command{
	Use:     "subscription <command>",
	Aliases: []string{"sub"},
	Short:   "Manage subscriptions",
	GroupID: cmdGroup,
}

var subAddCmd = &cobra.Command{
	Use:   "add <url>",
	Short: "Add a subscription",
	Args:  cobra.ExactArgs(1),
}

func (a *app) subAdd(cmd *cobra.Command, args []string) error {
	stop := spin("Fetching subscription")
	sub, err := a.client.AddSub(args[0])
	stop()
	if err != nil {
		return err
	}
	done("Added " + a.clean(sub.Name))
	fields([2]string{"ID", sub.ID}, [2]string{"Nodes", strconv.Itoa(sub.Nodes)}, [2]string{"Traffic", style.Usage(sub.Traffic)})
	return nil
}

var subRemoveCmd = &cobra.Command{
	Use:   "remove <id | name>",
	Short: "Remove a subscription",
	Args:  cobra.ExactArgs(1),
}

func (a *app) subRemove(cmd *cobra.Command, args []string) error {
	sub, err := a.resolveSub(args[0])
	if err != nil {
		return err
	}
	name := a.clean(sub.Name)
	stop := spin("Removing " + name)
	err = a.client.RemoveSub(sub.ID)
	stop()
	if err != nil {
		return err
	}
	done("Removed " + name)
	return nil
}

var subRefreshCmd = &cobra.Command{
	Use:   "refresh [id | name]",
	Short: "Refresh subscriptions",
	Args:  cobra.MaximumNArgs(1),
}

func (a *app) subRefresh(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		snapshot, err := a.client.Snapshot()
		if err != nil {
			return err
		}
		if len(snapshot.Subscriptions) == 0 {
			out(style.Dim.Render("No subscriptions yet. Add one: " + cmd.Parent().CommandPath() + " add <url>"))
			return nil
		}
		stop := spin("Refreshing subscriptions")
		err = a.client.RefreshAll()
		stop()
		if err != nil {
			return err
		}
		done("Refreshed all subscriptions")
		return nil
	}

	sub, err := a.resolveSub(args[0])
	if err != nil {
		return err
	}
	name := a.clean(sub.Name)
	stop := spin("Refreshing " + name)
	err = a.client.Refresh(sub.ID)
	stop()
	if err != nil {
		return err
	}
	done("Refreshed " + name)
	if snap, err := a.client.Snapshot(); err == nil {
		for _, s := range snap.Subscriptions {
			if s.ID == sub.ID {
				fields([2]string{"ID", s.ID}, [2]string{"Nodes", strconv.Itoa(s.Nodes)}, [2]string{"Traffic", style.Usage(s.Traffic)})
				break
			}
		}
	}
	return nil
}

var subListCmd = &cobra.Command{
	Use:   "list",
	Short: "List subscriptions and their nodes",
	Args:  cobra.NoArgs,
}

func (a *app) subList(cmd *cobra.Command, args []string) error {
	snapshot, err := a.client.Snapshot()
	if err != nil {
		return err
	}
	subs := snapshot.Subscriptions
	if jsonOut, _ := cmd.Flags().GetBool("json"); jsonOut {
		type nodeOut struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Protocol string `json:"protocol"`
			Server   string `json:"server"`
			Port     int    `json:"port"`
			Probed   bool   `json:"probed,omitempty"`
			Alive    bool   `json:"alive,omitempty"`
			MS       int    `json:"ms,omitempty"`
		}
		type subOut struct {
			ID      string          `json:"id"`
			Name    string          `json:"name"`
			Traffic *domain.Traffic `json:"traffic,omitempty"`
			Nodes   []nodeOut       `json:"nodes"`
		}
		groups := (tree.Data{Subs: subs, Nodes: snapshot.Nodes}).Groups()
		result := make([]subOut, len(groups))
		for i, g := range groups {
			nodes := make([]nodeOut, len(g.Nodes))
			for j, n := range g.Nodes {
				nodes[j] = nodeOut{n.ID, n.Name, n.Protocol, n.Server, n.Port, n.Probed, n.Alive, n.MS}
			}
			s := subOut{ID: g.Sub.ID, Name: g.Sub.Name, Nodes: nodes}
			if tr := g.Sub.Traffic; tr.TotalBytes > 0 || tr.UploadBytes > 0 || tr.DownloadBytes > 0 {
				s.Traffic = &tr
			}
			result[i] = s
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	if len(subs) == 0 {
		out(style.Dim.Render("No subscriptions yet. Add one: " + cmd.Parent().CommandPath() + " add <url>"))
		return nil
	}
	a.showTree(subs, snapshot.Nodes)
	return nil
}

func init() {
	subCmd.AddCommand(subAddCmd, subRemoveCmd, subRefreshCmd, subListCmd)
	subListCmd.Flags().Bool("json", false, "Output subscriptions as JSON")
}

func (a *app) resolveSub(key string) (ipc.Sub, error) {
	snapshot, err := a.client.Snapshot()
	if err != nil {
		return ipc.Sub{}, err
	}
	return match(key, "subscription", snapshot.Subscriptions, func(s ipc.Sub) (string, string) { return s.ID, s.Name })
}

func (a *app) completeSub(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	c := a.daemon()
	if c == nil || c.Ping() != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	snapshot, err := c.Snapshot()
	return completeNames(snapshot.Subscriptions, err, func(s ipc.Sub) string { return s.Name })
}

func (a *app) showTree(subs []ipc.Sub, nodes []ipc.Node) {
	groups := (tree.Data{Subs: subs, Nodes: nodes}).Groups()
	for i, g := range groups {
		if i > 0 {
			out("")
		}
		if g.Sub.ID == tree.Default {
			out(style.Name.Render(a.clean(g.Sub.Name)))
		} else {
			out(style.Name.Render(a.clean(g.Sub.Name)) + "  " + style.Dim.Render(g.Sub.ID))
			out(style.Usage(g.Sub.Traffic) + style.Dim.Render(" "+style.Sep()+" updated "+style.Since(g.Sub.UpdatedAt)))
		}

		nameW, infoW := 0, 0
		for _, n := range g.Nodes {
			nameW = max(nameW, lipgloss.Width(a.nodeName(n.Name, "")))
			infoW = max(infoW, lipgloss.Width(a.serverProto(n)))
		}
		for j, n := range g.Nodes {
			branch := style.Branch(j == len(g.Nodes)-1)
			out(a.nodeLine(n, branch, nameW, infoW))
		}
	}
}

func (a *app) nodeLine(n ipc.Node, branch string, nameW, infoW int) string {
	name := style.Pad(a.nodeName(n.Name, ""), nameW)
	info := style.Dim.Render(style.Pad(a.serverProto(n), infoW))
	id := style.Dim.Render(style.Pad(displayID(n.ID), 8))
	prefix := "  "
	if branch != "" {
		prefix = style.Dim.Render(branch) + " "
	}
	line := fmt.Sprintf("%s%s  %s  %s", prefix, name, info, id)
	if n.Probed {
		if n.Alive {
			line += "  " + style.Alive.Render(fmt.Sprintf("%dms", n.MS))
		} else {
			line += "  " + style.Dead.Render("t/o")
		}
	}
	return line
}

func (a *app) serverProto(n ipc.Node) string {
	return fmt.Sprintf("%s:%d %s %s", a.clean(n.Server), n.Port, style.Sep(), n.Protocol)
}
