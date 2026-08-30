package cli

import (
	"fmt"
	"strconv"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"github.com/luynrs/justray/internal/client/tui/style"
	"github.com/luynrs/justray/internal/client/tui/tree"
	"github.com/luynrs/justray/internal/shared/rpc"
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
	snapshot, err := a.client.AddSub(args[0])
	stop()
	if err != nil {
		return err
	}
	if len(snapshot.Subscriptions) == 0 {
		return fmt.Errorf("subscription was not added")
	}
	sub := snapshot.Subscriptions[len(snapshot.Subscriptions)-1]
	done("Added " + a.clean(sub.Name))
	f := [][2]string{{"ID", sub.ID}, {"Nodes", strconv.Itoa(sub.Nodes)}}
	if t := style.Usage(sub.Traffic); t != "" {
		f = append(f, [2]string{"Traffic", t})
	}
	fields(f...)
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
	_, err = a.client.RemoveSub(sub.ID)
	stop()
	if err != nil {
		return err
	}
	done("Removed " + name)
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
	if len(subs) == 0 {
		out(style.Dim.Render("No subscriptions yet. Add one: " + cmd.Parent().CommandPath() + " add <url>"))
		return nil
	}
	a.showTree(subs, snapshot.Nodes)
	return nil
}

func init() {
	subCmd.AddCommand(subAddCmd, subRemoveCmd, subListCmd)
}

func (a *app) resolveSub(key string) (rpc.Sub, error) {
	snapshot, err := a.client.Snapshot()
	if err != nil {
		return rpc.Sub{}, err
	}
	return match(key, "subscription", snapshot.Subscriptions, func(s rpc.Sub) (string, string) { return s.ID, s.Name })
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
	return completeNames(snapshot.Subscriptions, err, func(s rpc.Sub) string { return s.Name })
}

func (a *app) showTree(subs []rpc.Sub, nodes []rpc.Node) {
	groups := (tree.Data{Subs: subs, Nodes: nodes}).Groups()
	for i, g := range groups {
		if i > 0 {
			out("")
		}
		if g.Sub.ID == tree.Default {
			out(style.Name.Render(a.clean(g.Sub.Name)))
		} else {
			out(style.Name.Render(a.clean(g.Sub.Name)) + "  " + style.Dim.Render(g.Sub.ID))
			out(subMeta(g.Sub))
		}

		nameW, infoW := 0, 0
		for _, n := range g.Nodes {
			nameW = max(nameW, lipgloss.Width(a.nodeName(n.Name, "")))
			infoW = max(infoW, lipgloss.Width(a.serverProto(n)))
		}
		for j, n := range g.Nodes {
			branch := "├─"
			if j == len(g.Nodes)-1 {
				branch = "└─"
			}
			out(a.nodeLine(n, branch, nameW, infoW))
		}
	}
}

func (a *app) nodeLine(n rpc.Node, branch string, nameW, infoW int) string {
	name := style.Pad(a.nodeName(n.Name, ""), nameW)
	info := style.Dim.Render(style.Pad(a.serverProto(n), infoW))
	id := style.Dim.Render(displayID(n.ID))
	if branch == "" {
		return fmt.Sprintf("%s  %s  %s", a.nodeName(n.Name, ""), info, id)
	}
	return fmt.Sprintf("%s %s  %s  %s", style.Dim.Render(branch), name, info, id)
}

func (a *app) serverProto(n rpc.Node) string {
	return fmt.Sprintf("%s:%d · %s", a.clean(n.Server), n.Port, n.Protocol)
}

func subMeta(s rpc.Sub) string {
	meta := style.Usage(s.Traffic)
	if meta != "" {
		meta += style.Dim.Render(" · ")
	}
	return meta + style.Dim.Render("updated "+style.Since(s.UpdatedAt))
}
