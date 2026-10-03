package cli

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/luynrs/justray/internal/domain"
	"github.com/luynrs/justray/internal/ipc"
)

var upCmd = &cobra.Command{
	Use:     "up [id | name]",
	Short:   "Connect",
	GroupID: cmdGroup,
	Args:    cobra.MaximumNArgs(1),
}

func (a *app) up(cmd *cobra.Command, args []string) error {
	tun, _ := cmd.Flags().GetBool("tun")
	proxy, _ := cmd.Flags().GetBool("proxy")
	if tun && proxy {
		return fmt.Errorf("cannot use both --tun and --proxy")
	}
	var mode *bool
	if tun || proxy {
		mode = &tun
	}
	ctx := cmd.Context()

	snapshot, err := a.client.Snapshot(ctx)
	if err != nil {
		return err
	}
	nodes := snapshot.Nodes
	var key string
	if len(args) > 0 {
		key = args[0]
	} else {
		if snapshot.Status.Connected {
			if mode != nil {
				return a.switchMode(ctx, snapshot.Status, *mode)
			}
			a.report(snapshot)
			return nil
		}
		if snapshot.Selected.NodeID == "" {
			return fmt.Errorf("no node selected yet; pick one: %s <id | name>", cmd.CommandPath())
		}
		key = snapshot.Selected.NodeID
		if snapshot.Selected.SubscriptionID != "" {
			nodes = slices.DeleteFunc(nodes, func(node ipc.Node) bool { return node.SubscriptionID != snapshot.Selected.SubscriptionID })
		}
	}
	node, err := match(key, "node", nodes, func(node ipc.Node) (string, string) { return node.NodeID, node.Name })
	if err != nil {
		return err
	}
	return a.connect(ctx, node, mode)
}

func init() {
	upCmd.Flags().Bool("tun", false, "Connect in TUN mode")
	upCmd.Flags().Bool("proxy", false, "Connect in proxy mode")
}

func (a *app) connect(ctx context.Context, n ipc.Node, mode *bool) error {
	spinText := "Connecting to " + a.clean(n.Name)
	snapshot, err := a.runOp(ctx, spinText, func() error {
		return a.client.Connect(ctx, n.Ref(), mode)
	}, n.Ref(), mode)
	if err != nil {
		return err
	}
	a.report(snapshot)
	return nil
}

func (a *app) runOp(ctx context.Context, text string, op func() error, ref domain.NodeRef, want *bool) (ipc.Snapshot, error) {
	stop := spin(text)
	err := op()
	stop()
	if err == nil {
		snapshot, err := a.client.Snapshot(ctx)
		st := snapshot.Status
		if err == nil && (!st.Connected || st.NodeRef != ref || want != nil && st.Tun != *want) {
			return snapshot, errors.New("requested connection is not active")
		}
		return snapshot, err
	}
	if !errors.Is(err, ipc.ErrElevate) {
		return ipc.Snapshot{}, err
	}
	stop = spin("Granting permissions")
	defer stop()
	if want == nil {
		tun := true
		want = &tun
	}
	return a.client.AwaitConnection(ctx, ref, want)
}

func (a *app) switchMode(ctx context.Context, st ipc.Status, tun bool) error {
	next, err := a.runOp(ctx, "Switching to "+strings.ToUpper(modeWord(tun)), func() error {
		return a.client.SetTun(ctx, tun)
	}, st.NodeRef, &tun)
	if err != nil {
		return err
	}
	a.report(next)
	return nil
}

func (a *app) report(snapshot ipc.Snapshot) {
	done(upperFirst(state(snapshot.Status)))
	a.nodeDetails(snapshot.Status, snapshot.Nodes)
}
