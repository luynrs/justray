package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"github.com/luynrs/justray/internal/client/cli/detach"
	"github.com/luynrs/justray/internal/client/tui"
	"github.com/luynrs/justray/internal/client/tui/style"
	"github.com/luynrs/justray/internal/daemon/store"
	"github.com/luynrs/justray/internal/ipc"
	"github.com/luynrs/justray/internal/logger"
	"github.com/luynrs/justray/internal/platform/elevate"
	"github.com/luynrs/justray/internal/platform/lock"
	"github.com/luynrs/justray/internal/version"
)

// per-run CLI state
type app struct {
	client *ipc.Client
	emoji  bool
}

const cmdGroup = "commands"

var rootCmd = &cobra.Command{
	Use:     "justray <command>",
	Long:    `A modern VPN client that lives in your terminal`,
	Version: version.String(),

	SilenceErrors: true,
	SilenceUsage:  true,
}

func cmdLine(c *cobra.Command) string {
	return fmt.Sprintf("%-*s", c.NamePadding()+1, c.Name()+":")
}

const usageTemplate = `{{bold "USAGE"}}
  {{.UseLine}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

{{bold "AVAILABLE COMMANDS"}}{{range $cmds}}{{if .IsAvailableCommand}}
  {{cmdLine .}} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{bold .Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) .IsAvailableCommand)}}
  {{cmdLine .}} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

{{bold "ADDITIONAL COMMANDS"}}{{range $cmds}}{{if (and (eq .GroupID "") .IsAvailableCommand)}}
  {{cmdLine .}} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

{{bold "FLAGS"}}
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

{{bold "GLOBAL FLAGS"}}
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{.CommandPath}} <command> --help" for more information about a command.{{end}}
`

func init() {
	cobra.EnableCommandSorting = false
	cobra.AddTemplateFunc("bold", style.Name.Render)
	cobra.AddTemplateFunc("cmdLine", cmdLine)
	cobra.AddTemplateFunc("versionBlock", versionBlock)
	rootCmd.SetUsageTemplate(usageTemplate)
	rootCmd.SetVersionTemplate("{{versionBlock}}")
	rootCmd.AddGroup(&cobra.Group{ID: cmdGroup, Title: "AVAILABLE COMMANDS"})
	rootCmd.AddCommand(upCmd, downCmd, stopCmd, probeCmd, statusCmd, subCmd, logsCmd, versionCmd)
}

// Execute runs the justray CLI. The caller (cmd/justray) handles the error.
func Execute() error {
	style.TTY = style.DetectTTY("")
	a := &app{}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	rootCmd.Use = filepath.Base(os.Args[0]) + " <command>"
	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		for c := cmd; c != nil; c = c.Parent() {
			if c.Name() == cobra.ShellCompRequestCmd || c.Name() == "completion" || c.Name() == "help" || c.Name() == "stop" || c.Name() == "version" || c.Name() == "logs" {
				return nil
			}
		}
		if cmd == rootCmd {
			dir, err := ipc.Dir()
			if err != nil {
				return err
			}
			a.client = ipc.NewClient(ipc.Socket(dir))
			return nil
		}
		if err := a.connectDaemon(cmd.Context(), cmd != statusCmd && cmd != subListCmd && cmd != downCmd, false); err != nil {
			return err
		}
		if snapshot, err := a.client.Snapshot(cmd.Context()); err == nil {
			a.emoji = snapshot.Settings.Emoji == "on"
			style.TTY = style.DetectTTY(snapshot.Settings.ForceTTY)
		}
		return nil
	}
	rootCmd.RunE = func(cmd *cobra.Command, args []string) error {
		return tui.Run(a.client, a.start, a.restore)
	}
	upCmd.RunE = a.up
	downCmd.RunE = a.down
	stopCmd.RunE = a.stop
	probeCmd.RunE = a.probe
	statusCmd.RunE = a.status
	subAddCmd.RunE = a.subAdd
	subRemoveCmd.RunE = a.subRemove
	subRefreshCmd.RunE = a.subRefresh
	subListCmd.RunE = a.subList
	logsCmd.RunE = a.logs
	upCmd.ValidArgsFunction = a.completeNode
	subRemoveCmd.ValidArgsFunction = a.completeSub
	subRefreshCmd.ValidArgsFunction = a.completeSub
	probeCmd.ValidArgsFunction = a.completeProbe

	rootCmd.SetOut(lipgloss.Writer)
	rootCmd.InitDefaultVersionFlag()
	rootCmd.Flags().Lookup("version").Usage = "Show version"
	rootCmd.InitDefaultCompletionCmd()
	for _, c := range rootCmd.Commands() {
		if c.Name() == "completion" {
			c.Short = "Generate shell completion scripts"
		}
	}
	setHelpText(rootCmd)

	err := rootCmd.ExecuteContext(ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return errors.New(a.clean(err.Error()))
	}
	return nil
}

func setHelpText(c *cobra.Command) {
	c.InitDefaultHelpFlag()
	c.Flags().Lookup("help").Usage = "Show help for command"
	for _, sub := range c.Commands() {
		setHelpText(sub)
	}
}

func (a *app) start(ctx context.Context) error {
	return a.connectDaemon(ctx, true, false)
}

func (a *app) restore(ctx context.Context) error {
	return a.connectDaemon(ctx, true, true)
}

func (a *app) connectDaemon(ctx context.Context, startMissing, restore bool) error {
	caller := ctx
	dir, err := ipc.Dir()
	if err != nil {
		return fmt.Errorf("resolve config dir: %w", err)
	}
	if a.client == nil {
		a.client = ipc.NewClient(ipc.Socket(dir))
	}
	if !startMissing {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			return ctx.Err()
		}
	}
	if err := ipc.EnsureDir(dir); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	for delay := 5 * time.Millisecond; ; delay = min(delay*2, 100*time.Millisecond) {
		if err := ctx.Err(); err != nil {
			return err
		}
		unlock, err := lock.File(ipc.Socket(dir) + ".client.lock")
		if err == nil {
			defer unlock()
			break
		}
		if !errors.Is(err, lock.ErrLocked) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	pingErr := a.client.Ping(ctx)
	replacing := errors.Is(pingErr, ipc.ErrVersion)
	if pingErr != nil {
		if !replacing && !errors.Is(pingErr, ipc.ErrNoDaemon) {
			return pingErr
		}
		if !startMissing && !replacing {
			return nil
		}
		bin, err := justrayd(ctx)
		if err != nil {
			return err
		}
		if replacing {
			if err := ctx.Err(); err != nil {
				return err
			}
			var finish context.CancelFunc
			ctx, finish = context.WithTimeout(context.WithoutCancel(ctx), 45*time.Second)
			defer finish()
			shutdownErr := a.client.Shutdown(ctx)
			if err := waitStopped(ctx, ipc.Socket(dir), 6*time.Second); err != nil {
				return fmt.Errorf("restart background service: %w", errors.Join(shutdownErr, err))
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := spawn(bin, dir); err != nil {
			return fmt.Errorf("start background service: %w", err)
		}
		err = wait(ctx, a.client, 10*time.Second)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return err
			}
			return fmt.Errorf("daemon did not start: %w; see %s", err, ipc.DaemonLog(dir))
		}
	}
	if err := caller.Err(); err != nil {
		return err
	}
	if !restore {
		return caller.Err()
	}
	ctx = caller
	snapshot, err := a.client.Snapshot(ctx)
	if err != nil {
		return err
	}
	if !snapshot.Status.Connected {
		state, err := (store.Disk{Dir: dir}).Load()
		if err != nil {
			return fmt.Errorf("restore connection: %w", err)
		}
		if state.Active.NodeID != "" {
			err := a.client.Connect(ctx, state.Active, &state.Tun)
			if errors.Is(err, ipc.ErrElevate) {
				_, err = a.client.AwaitConnection(context.WithoutCancel(ctx), state.Active, &state.Tun, 30*time.Second)
				if err := caller.Err(); err != nil {
					return err
				}
			}
			if err != nil {
				return fmt.Errorf("restore connection: %w", err)
			}
		}
	}
	return ctx.Err()
}

func spawn(bin, dir string) error {
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer func() { _ = devNull.Close() }()

	errLog, err := logger.Open(ipc.DaemonLog(dir))
	if err != nil {
		return err
	}
	defer func() { _ = errLog.Close() }()

	cmd := exec.Command(elevate.Executable(bin, dir))
	cmd.Args[0] = bin
	cmd.Stdin, cmd.Stdout, cmd.Stderr = devNull, devNull, errLog
	detach.Cmd(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }() // reap the daemon when it exits, or it lingers as a zombie
	return nil
}

func justrayd(ctx context.Context) (string, error) {
	path, _ := exec.LookPath(exeName("justrayd"))
	candidates := []string{path, nextToSelf("justrayd")}
	if dir, err := ipc.Dir(); err == nil {
		candidates = append(candidates, filepath.Join(dir, "elevated", exeName("justrayd")))
	}
	for _, bin := range candidates {
		if bin == "" {
			continue
		}
		check, cancel := context.WithTimeout(ctx, 3*time.Second)
		output, err := exec.CommandContext(check, bin, "--version").Output()
		cancel()
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if err == nil && strings.TrimSpace(string(output)) == "justrayd "+version.String() {
			return bin, nil
		}
	}
	return "", errors.New("matching background service is missing; reinstall justray")
}

func exeName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func nextToSelf(name string) string {
	self, err := os.Executable()
	if err != nil {
		return ""
	}
	p := filepath.Join(filepath.Dir(self), exeName(name))
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

func wait(ctx context.Context, c *ipc.Client, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for delay := 5 * time.Millisecond; ; delay = min(delay*2, 100*time.Millisecond) {
		if err := c.Ping(ctx); err == nil || errors.Is(err, ipc.ErrVersion) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
}

// daemon dials silently, for completions — no spawn, no error reporting
func (a *app) daemon() *ipc.Client {
	if a.client == nil {
		if d, err := ipc.Dir(); err == nil {
			a.client = ipc.NewClient(ipc.Socket(d))
		}
	}
	return a.client
}

var errNotFound = errors.New("not found")

func match[T any](key, noun string, items []T, idName func(T) (id, name string)) (T, error) {
	key = strings.ToLower(key)
	var hits []T
	var names []string
	for _, it := range items {
		id, name := idName(it)
		idLower := strings.ToLower(id)
		if idLower == key {
			return it, nil
		}
		if strings.HasPrefix(idLower, key) || strings.Contains(strings.ToLower(name), key) {
			hits = append(hits, it)
			names = append(names, fmt.Sprintf("%s (%s)", style.Sanitize(name, true), displayID(id)))
		}
	}
	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		var zero T
		return zero, fmt.Errorf("%w: no %s matches %q", errNotFound, noun, key)
	default:
		var zero T
		return zero, fmt.Errorf("%q matches %d %ss: %s", key, len(hits), noun, strings.Join(names, ", "))
	}
}

func completeNames[T any](items []T, err error, name func(T) string) ([]string, cobra.ShellCompDirective) {
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	names := make([]string, len(items))
	for i, it := range items {
		names[i] = name(it)
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}
