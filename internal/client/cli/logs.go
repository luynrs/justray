package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/luynrs/justray/internal/ipc"
)

var logsFollowFlag bool

var logsCmd = &cobra.Command{
	Use:       "logs [daemon | engine | tui]",
	Short:     "View logs",
	Args:      cobra.MaximumNArgs(1),
	ValidArgs: []string{"daemon", "engine", "tui"},
}

func init() {
	logsCmd.Flags().BoolVarP(&logsFollowFlag, "follow", "f", false, "Follow log output")
}

func (a *app) logs(cmd *cobra.Command, args []string) error {
	target := "daemon"
	if len(args) > 0 {
		target = strings.ToLower(args[0])
	}
	dir, err := ipc.Dir()
	if err != nil {
		return err
	}
	var logPath string
	switch target {
	case "daemon":
		logPath = ipc.DaemonLog(dir)
	case "engine":
		logPath = ipc.EngineLog(dir)
	case "tui":
		logPath = ipc.TUILog(dir)
	default:
		return fmt.Errorf("unknown log target %q; pick from daemon, engine, tui", target)
	}

	if !logsFollowFlag {
		f, err := os.Open(logPath)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		defer func() { _ = f.Close() }()
		_, err = io.Copy(cmd.OutOrStdout(), f)
		return err
	}

	ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	return followLog(ctx, logPath, cmd.OutOrStdout())
}

func followLog(ctx context.Context, path string, out io.Writer) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	var f *os.File
	var err error
	for {
		f, err = os.Open(path)
		if err == nil {
			break
		}
		if !os.IsNotExist(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
	defer func() { _ = f.Close() }()

	buf := make([]byte, 32*1024)
	for {
		n, rerr := f.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				return werr
			}
		}
		if rerr != nil {
			if rerr != io.EOF {
				return rerr
			}
			if fi, err := f.Stat(); err == nil {
				if cur, err := f.Seek(0, io.SeekCurrent); err == nil && fi.Size() < cur {
					_, _ = f.Seek(0, io.SeekStart)
				}
			}
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
			}
		}
	}
}
