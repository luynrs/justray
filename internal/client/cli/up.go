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
	mode := tunMode(tun, proxy)
	ctx := cmd.Context()

	if len(args) > 0 {
		return a.connectNode(ctx, args[0], mode)
	}

	snapshot, err := a.client.Snapshot()
	if err != nil {
		return err
	}
	st := snapshot.Status
	if st.Connected {
		if mode != nil {
			return a.switchMode(ctx, st, *mode)
		}
		a.report(upperFirst(state(st)), st)
		return nil
	}

	ref := snapshot.Selected
	if ref.NodeID == "" {
		return fmt.Errorf("no node selected yet; pick one: %s <id | name>", cmd.CommandPath())
	}
	n, err := a.resolveNode(ref.NodeID, ref.SubscriptionID)
	if err != nil {
		return err
	}
	return a.connect(ctx, n, mode)
}

func init() {
	upCmd.Flags().Bool("tun", false, "Connect in TUN mode")
	upCmd.Flags().Bool("proxy", false, "Connect in proxy mode")
}

func tunMode(tun, proxy bool) *bool {
	switch {
	case tun:
		return &tun
	case proxy:
		off := false
		return &off
	}
	return nil
}

func (a *app) connectNode(ctx context.Context, key string, mode *bool) error {
	n, err := a.resolveNode(key, "")
	if err != nil {
		return err
	}
	return a.connect(ctx, n, mode)
}

func (a *app) connect(ctx context.Context, n ipc.Node, mode *bool) error {
	spinText := "Connecting to " + a.clean(n.Name)
	st, err := a.runOp(ctx, spinText, func() error {
		return a.client.Connect(n.Ref(), mode)
	}, n.Ref(), mode)
	if err != nil {
		return err
	}
	a.report(upperFirst(state(st)), st)
	return nil
}

func (a *app) runOp(ctx context.Context, text string, op func() error, ref domain.NodeRef, want *bool) (ipc.Status, error) {
	status := func() (ipc.Status, error) {
		snapshot, err := a.client.Snapshot()
		return snapshot.Status, err
	}
	stop := spin(text)
	err := op()
	stop()
	if err == nil {
		st, err := status()
		if err == nil && (!st.Connected || st.NodeRef != ref || want != nil && st.Tun != *want) {
			return st, errors.New("requested connection is not active")
		}
		return st, err
	}
	if err.Error() != ipc.ErrElevate.Error() {
		return ipc.Status{}, err
	}
	stop = spin("Granting permissions")
	defer stop()
	if want == nil {
		tun := true
		want = &tun
	}
	return awaitElevate(ctx, status, ref, want, 30*time.Second)
}

var elevatePoll = 500 * time.Millisecond

func awaitElevate(ctx context.Context, status func() (ipc.Status, error), ref domain.NodeRef, want *bool, timeout time.Duration) (ipc.Status, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	pending := false
	for delay := min(5*time.Millisecond, elevatePoll); ; delay = min(delay*2, elevatePoll) {
		st, err := status()
		switch {
		case err != nil: // daemon mid exec-restart
			pending = true
		case st.Connected && st.NodeRef == ref && (want == nil || st.Tun == *want):
			return st, nil
		case pending:
			return st, errors.New("daemon restarted without requested connection")
		}
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return ipc.Status{}, errors.New("timed out waiting for permissions")
			}
			return ipc.Status{}, ctx.Err()
		case <-time.After(delay):
		}
	}
}

func (a *app) switchMode(ctx context.Context, st ipc.Status, tun bool) error {
	next, err := a.runOp(ctx, "Switching to "+strings.ToUpper(modeWord(tun)), func() error {
		return a.client.SetTun(tun)
	}, st.NodeRef, &tun)
	if err != nil {
		return err
	}
	a.report(upperFirst(state(next)), next)
	return nil
}

func (a *app) report(headline string, st ipc.Status) {
	done(headline)
	a.nodeDetails(st, nil)
}

func (a *app) resolveNode(key, sub string) (ipc.Node, error) {
	snapshot, err := a.client.Snapshot()
	if err != nil {
		return ipc.Node{}, err
	}
	nodes := snapshot.Nodes
	if sub != "" {
		nodes = slices.DeleteFunc(nodes, func(n ipc.Node) bool { return n.Sub != sub })
	}
	return match(key, "node", nodes, func(n ipc.Node) (string, string) { return n.ID, n.Name })
}

func (a *app) completeNode(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	c := a.daemon()
	if c == nil || c.Ping() != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	snapshot, err := c.Snapshot()
	return completeNames(snapshot.Nodes, err, func(n ipc.Node) string { return n.Name })
}
