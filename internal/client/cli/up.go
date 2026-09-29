package cli

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

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
		return fmt.Errorf("pick either --tun or --proxy")
	}
	var mode *bool
	if tun || proxy {
		mode = &tun
	}
	ctx := cmd.Context()

	if len(args) > 0 {
		return a.connectNode(ctx, args[0], mode)
	}

	snapshot, err := a.client.Snapshot(ctx)
	if err != nil {
		return err
	}
	st := snapshot.Status
	if st.Connected {
		if mode != nil {
			return a.switchMode(ctx, st, *mode)
		}
		a.report(snapshot)
		return nil
	}

	ref := snapshot.Selected
	if ref.NodeID == "" {
		return fmt.Errorf("no node selected yet; pick one: %s <id | name>", cmd.CommandPath())
	}
	n, err := a.resolveNode(ctx, ref.NodeID, ref.SubscriptionID)
	if err != nil {
		return err
	}
	return a.connect(ctx, n, mode)
}

func init() {
	upCmd.Flags().Bool("tun", false, "Connect in TUN mode")
	upCmd.Flags().Bool("proxy", false, "Connect in proxy mode")
}

func (a *app) connectNode(ctx context.Context, key string, mode *bool) error {
	n, err := a.resolveNode(ctx, key, "")
	if err != nil {
		return err
	}
	return a.connect(ctx, n, mode)
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
	return awaitElevate(ctx, a.client, ref, want, 30*time.Second)
}

func awaitElevate(ctx context.Context, client *ipc.Client, ref domain.NodeRef, want *bool, timeout time.Duration) (ipc.Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	pending := false
	for delay := 5 * time.Millisecond; ; delay = min(delay*2, 500*time.Millisecond) {
		snapshot, err := client.Snapshot(ctx)
		st := snapshot.Status
		switch {
		case err != nil: // daemon mid exec-restart
			pending = true
		case st.Connected && st.NodeRef == ref && (want == nil || st.Tun == *want):
			return snapshot, nil
		case pending:
			return snapshot, errors.New("daemon restarted without requested connection")
		}
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return ipc.Snapshot{}, errors.New("timed out waiting for permissions")
			}
			return ipc.Snapshot{}, ctx.Err()
		case <-time.After(delay):
		}
	}
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

func (a *app) resolveNode(ctx context.Context, key, sub string) (ipc.Node, error) {
	snapshot, err := a.client.Snapshot(ctx)
	if err != nil {
		return ipc.Node{}, err
	}
	nodes := snapshot.Nodes
	if sub != "" {
		nodes = slices.DeleteFunc(nodes, func(n ipc.Node) bool { return n.SubscriptionID != sub })
	}
	return match(key, "node", nodes, func(n ipc.Node) (string, string) { return n.NodeID, n.Name })
}

func (a *app) completeNode(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	c := a.daemon()
	if c == nil || c.Ping(cmd.Context()) != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	snapshot, err := c.Snapshot(cmd.Context())
	return completeNames(snapshot.Nodes, err, func(n ipc.Node) string { return n.Name })
}
