package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"github.com/luynrs/justray/internal/client/cli/detach"
	"github.com/luynrs/justray/internal/client/tui"
	"github.com/luynrs/justray/internal/client/tui/style"
	"github.com/luynrs/justray/internal/shared/rpc"
	"github.com/luynrs/justray/internal/shared/version"
)

// per-run CLI state
type app struct {
	client *rpc.Client
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
	rootCmd.AddCommand(upCmd, downCmd, statusCmd, subCmd)
}

// Execute runs the justray CLI. The caller (cmd/justray) handles the error.
func Execute() error {
	a := &app{}

	rootCmd.Use = filepath.Base(os.Args[0]) + " <command>"
	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		for c := cmd; c != nil; c = c.Parent() {
			if c.Name() == "completion" || c.Name() == "help" {
				return nil
			}
		}
		return a.connectDaemon()
	}
	rootCmd.RunE = func(cmd *cobra.Command, args []string) error {
		return tui.Run(a.client)
	}
	upCmd.RunE = a.up
	downCmd.RunE = a.down
	statusCmd.RunE = a.status
	subAddCmd.RunE = a.subAdd
	subRemoveCmd.RunE = a.subRemove
	subListCmd.RunE = a.subList
	upCmd.ValidArgsFunction = a.completeNode
	subRemoveCmd.ValidArgsFunction = a.completeSub

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

	err := rootCmd.Execute()
	if err != nil {
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

func (a *app) connectDaemon() error {
	dir, err := rpc.Dir()
	if err != nil {
		return fmt.Errorf("resolve config dir: %w", err)
	}
	if err := rpc.EnsureDir(dir); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	a.client = rpc.NewClient(rpc.Socket(dir))
	if a.client.Ping() != nil {
		if err := spawn(dir); err != nil {
			return fmt.Errorf("start daemon: %w", err)
		}
		stop := spin("Starting daemon")
		err = wait(a.client, 10*time.Second)
		stop()
		if err != nil {
			return fmt.Errorf("daemon did not start, see %s", rpc.DaemonLog(dir))
		}
	}
	if s, err := a.client.Settings(); err == nil {
		a.emoji = s.Emoji == "on"
	}
	return nil
}

func spawn(dir string) error {
	bin, err := justrayd()
	if err != nil {
		return err
	}
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer func() { _ = devNull.Close() }()

	// a panic writes straight to stderr, past the logger — keep it in the log
	errLog, err := os.OpenFile(rpc.DaemonLog(dir), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = errLog.Close() }()

	cmd := exec.Command(bin)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = devNull, devNull, errLog
	detach.Cmd(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }() // reap the daemon when it exits, or it lingers as a zombie
	return nil
}

func justrayd() (string, error) {
	if bin := nextToSelf("justrayd"); bin != "" {
		return bin, nil
	}
	bin, err := exec.LookPath(exeName("justrayd"))
	if err != nil {
		return "", fmt.Errorf("justrayd not found next to justray or in PATH; build it with \"go build ./cmd/justrayd\"")
	}
	return bin, nil
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

func wait(c *rpc.Client, timeout time.Duration) error {
	for deadline := time.Now().Add(timeout); time.Now().Before(deadline); {
		if c.Ping() == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("daemon did not come up within %s", timeout)
}

// daemon dials silently, for completions — no spawn, no error reporting
func (a *app) daemon() *rpc.Client {
	if a.client == nil {
		if d, err := rpc.Dir(); err == nil {
			a.client = rpc.NewClient(rpc.Socket(d))
		}
	}
	return a.client
}

func match[T any](key, noun string, items []T, idName func(T) (id, name string)) (T, error) {
	key = strings.ToLower(key)
	var hits []T
	var names []string
	for _, it := range items {
		id, name := idName(it)
		if id == key {
			return it, nil
		}
		if strings.HasPrefix(id, key) || strings.Contains(strings.ToLower(name), key) {
			hits = append(hits, it)
			names = append(names, fmt.Sprintf("%s (%s)", style.Sanitize(name, true), displayID(id)))
		}
	}
	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		var zero T
		return zero, fmt.Errorf("no %s matches %q", noun, key)
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
