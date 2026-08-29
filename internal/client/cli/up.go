package cli

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/luynrs/justray/internal/shared/rpc"
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

	st, err := a.client.Status()
	if err != nil {
		return err
	}
	if st.Connected {
		if mode != nil {
			return a.switchMode(st, *mode)
		}
		a.report("Already "+state(st), st)
		return nil
	}

	ref, err := a.client.Active()
	if err != nil {
		return err
	}
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

func (a *app) connect(n rpc.Node, mode *bool) error {
	spinText := "Connecting to " + a.clean(n.Name)
	if mode != nil {
		if _, err := a.runOp(spinText, func() (rpc.Status, error) { return a.client.SetTun(*mode) }, mode); err != nil {
			return err
		}
	}
	st, err := a.runOp(spinText, func() (rpc.Status, error) {
		return a.client.Connect(n.Ref())
	}, mode)
	if err != nil {
		return err
	}
	a.report(upperFirst(state(st)), st)
	return nil
}

// runOp waits out the daemon re-execing itself with tun caps
func (a *app) runOp(text string, op func() (rpc.Status, error), want *bool) (rpc.Status, error) {
	stop := spin(text)
	st, err := op()
	stop()
	if err == nil || err.Error() != rpc.ErrElevate.Error() {
		return st, err
	}
	stop = spin("Granting permissions")
	defer stop()
	st, err = awaitElevate(a.client.Status, want, 3*time.Minute)
	if err == nil && want != nil && (!st.Connected || st.Tun != *want) {
		return op()
	}
	return st, err
}

var elevatePoll = 500 * time.Millisecond

func awaitElevate(status func() (rpc.Status, error), want *bool, timeout time.Duration) (rpc.Status, error) {
	pending := false
	for deadline := time.Now().Add(timeout); time.Now().Before(deadline); {
		time.Sleep(elevatePoll)
		st, err := status()
		switch {
		case err != nil: // the daemon is mid exec-restart
			pending = true
		case st.LastErr == rpc.ErrElevate.Error():
			pending = true
		case st.LastErr != "":
			return st, errors.New(st.LastErr)
		case pending:
			return st, nil
		case st.Connected && (want == nil || st.Tun == *want):
			return st, nil
		}
	}
	return rpc.Status{}, errors.New("timed out waiting for permissions")
}

func (a *app) switchMode(st rpc.Status, tun bool) error {
	if st.Tun == tun {
		a.report("Already "+state(st), st)
		return nil
	}
	next, err := a.runOp("Switching to "+strings.ToUpper(modeWord(tun)), func() (rpc.Status, error) {
		return a.client.SetTun(tun)
	}, &tun)
	if err != nil {
		return err
	}
	a.report(upperFirst(state(next)), next)
	return nil
}

func (a *app) report(headline string, st rpc.Status) {
	done(headline)
	a.nodeDetails(st)
	a.warn(st.LastErr)
}

func (a *app) resolveNode(key, sub string) (rpc.Node, error) {
	nodes, err := a.client.Nodes()
	if err != nil {
		return rpc.Node{}, err
	}
	if sub != "" {
		nodes = slices.DeleteFunc(nodes, func(n rpc.Node) bool { return n.Sub != sub })
	}
	return match(key, "node", nodes, func(n rpc.Node) (string, string) { return n.ID, n.Name })
}

func (a *app) completeNode(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	c := a.daemon()
	if c == nil || c.Ping() != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	nodes, err := c.Nodes()
	return completeNames(nodes, err, func(n rpc.Node) string { return n.Name })
}
