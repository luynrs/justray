package cli

import (
	"errors"
	"fmt"
	"slices"

	"github.com/spf13/cobra"

	"github.com/luynrs/justray/internal/client/tui/style"
	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/ipc"
)

var probeCmd = &cobra.Command{
	Use:     "probe [sub | id | name]",
	Short:   "Probe node latencies",
	GroupID: cmdGroup,
	Args:    cobra.MaximumNArgs(1),
}

func (a *app) probe(cmd *cobra.Command, args []string) error {
	snapshot, err := a.client.Snapshot()
	if err != nil {
		return err
	}
	if len(snapshot.Nodes) == 0 {
		out(style.Dim.Render("No nodes to probe. Add a subscription first: " + cmd.Root().CommandPath() + " subscription add <url>"))
		return nil
	}

	var subID, nodeID, spinnerText string
	if len(args) == 0 {
		spinnerText = "Probing nodes"
	} else {
		key := args[0]
		sub, subErr := match(key, "subscription", snapshot.Subscriptions, func(s ipc.Sub) (string, string) { return s.ID, s.Name })
		if subErr == nil {
			subID = sub.ID
			spinnerText = "Probing " + a.clean(sub.Name)
		} else if !errors.Is(subErr, errNotFound) {
			return subErr
		} else {
			node, nodeErr := match(key, "node", snapshot.Nodes, func(n ipc.Node) (string, string) { return n.ID, n.Name })
			if nodeErr == nil {
				subID = node.Sub
				nodeID = node.ID
				spinnerText = "Probing " + a.clean(node.Name)
			} else if !errors.Is(nodeErr, errNotFound) {
				return nodeErr
			} else {
				return fmt.Errorf("no subscription or node matching %q", key)
			}
		}
	}

	stop := spin(spinnerText)
	err = a.client.Probe(subID, nodeID)
	stop()
	if err != nil {
		return err
	}

	snap, err := a.client.Snapshot()
	if err != nil {
		return err
	}

	if nodeID != "" {
		n := a.lookupNode(domain.NodeRef{SubscriptionID: subID, NodeID: nodeID}, snap.Nodes)
		if n.ID == "" {
			return fmt.Errorf("node %q not found", nodeID)
		}
		done("Probed " + a.clean(n.Name))
		lat := style.Dead.Render("t/o")
		if n.Alive {
			lat = style.Alive.Render(fmt.Sprintf("%dms", n.MS))
		}
		fields(append([][2]string{{"Latency", lat}}, a.nodeFields(n)...)...)
		return nil
	}

	nodes := snap.Nodes
	subs := snap.Subscriptions
	if subID != "" {
		nodes = slices.DeleteFunc(nodes, func(n ipc.Node) bool { return n.Sub != subID })
		subs = slices.DeleteFunc(subs, func(s ipc.Sub) bool { return s.ID != subID })
		name := subID
		if len(subs) > 0 {
			name = a.clean(subs[0].Name)
		}
		done("Probed " + name)
	} else {
		noun := "nodes"
		if len(nodes) == 1 {
			noun = "node"
		}
		done(fmt.Sprintf("Probed %d %s", len(nodes), noun))
	}
	a.showTree(subs, nodes)
	return nil
}

func (a *app) completeProbe(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	c := a.daemon()
	if c == nil || c.Ping() != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	snap, err := c.Snapshot()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	names := make([]string, 0, len(snap.Subscriptions)+len(snap.Nodes))
	for _, s := range snap.Subscriptions {
		names = append(names, s.Name)
	}
	for _, n := range snap.Nodes {
		names = append(names, n.Name)
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}
