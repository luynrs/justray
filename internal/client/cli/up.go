package cli

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/luynrs/justray/internal/ipc"
)

var upTunFlag, upProxyFlag bool

var upCmd = &cobra.Command{
	Use:     "up [id | name]",
	Short:   "Connect",
	GroupID: cmdGroup,
	Args:    cobra.MaximumNArgs(1),
}

func (a *app) up(cmd *cobra.Command, args []string) error {
	if upTunFlag && upProxyFlag {
		return fmt.Errorf("pick either --tun or --proxy")
	}
	mode := tunMode(upTunFlag, upProxyFlag)

	if len(args) > 0 {
		return a.connectNode(args[0], mode)
	}

	snapshot, err := a.client.Snapshot()
	if err != nil {
		return err
	}
	st := snapshot.Status
	if st.Connected {
		if mode != nil {
			return a.switchMode(st, *mode)
		}
		a.report(upperFirst(state(st)), st)
		return nil
	}

	ref := snapshot.Active
	if ref.NodeID == "" {
		return fmt.Errorf("no node selected yet; pick one: %s <id | name>", cmd.CommandPath())
	}
	n, err := a.resolveNode(ref.NodeID, ref.SubscriptionID)
	if err != nil {
		return err
	}
	return a.connect(n, mode)
}

func init() {
	upCmd.Flags().BoolVar(&upTunFlag, "tun", false, "connect in TUN mode")
	upCmd.Flags().BoolVar(&upProxyFlag, "proxy", false, "connect in proxy mode")
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

func (a *app) connectNode(key string, mode *bool) error {
	n, err := a.resolveNode(key, "")
	if err != nil {
		return err
	}
	return a.connect(n, mode)
}

func (a *app) connect(n ipc.Node, mode *bool) error {
	spinText := "Connecting to " + a.clean(n.Name)
	if mode != nil {
		if _, err := a.runOp(spinText, func() (ipc.Snapshot, error) { return a.client.SetTun(*mode) }, mode); err != nil {
			return err
		}
	}
	st, err := a.runOp(spinText, func() (ipc.Snapshot, error) {
		return a.client.Connect(n.Ref())
	}, mode)
	if err != nil {
		return err
	}
	a.report(upperFirst(state(st)), st)
	return nil
}

// runOp waits out the daemon re-execing itself with tun caps
func (a *app) runOp(text string, op func() (ipc.Snapshot, error), want *bool) (ipc.Status, error) {
	stop := spin(text)
	snapshot, err := op()
	stop()
	if err == nil || err.Error() != ipc.ErrElevate.Error() {
		return snapshot.Status, err
	}
	stop = spin("Granting permissions")
	defer stop()
	st, err := awaitElevate(func() (ipc.Status, error) {
		snapshot, err := a.client.Snapshot()
		return snapshot.Status, err
	}, want, 30*time.Second)
	if err == nil && want != nil && (!st.Connected || st.Tun != *want) {
		snapshot, err := op()
		return snapshot.Status, err
	}
	return st, err
}

var elevatePoll = 500 * time.Millisecond

func awaitElevate(status func() (ipc.Status, error), want *bool, timeout time.Duration) (ipc.Status, error) {
	pending := false
	for deadline := time.Now().Add(timeout); time.Now().Before(deadline); {
		time.Sleep(elevatePoll)
		st, err := status()
		switch {
		case err != nil: // the daemon is mid exec-restart
			pending = true
		case pending:
			return st, nil
		case st.Connected && (want == nil || st.Tun == *want):
			return st, nil
		}
	}
	return ipc.Status{}, errors.New("timed out waiting for permissions")
}

func (a *app) switchMode(st ipc.Status, tun bool) error {
	if st.Tun == tun {
		a.report(upperFirst(state(st)), st)
		return nil
	}
	next, err := a.runOp("Switching to "+strings.ToUpper(modeWord(tun)), func() (ipc.Snapshot, error) {
		return a.client.SetTun(tun)
	}, &tun)
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
