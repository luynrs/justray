package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"

	"github.com/luynrs/justray/internal/ipc"
)

var logsCmd = &cobra.Command{
	Use:       "logs [daemon | engine | tui]",
	Short:     "View logs",
	Args:      cobra.MaximumNArgs(1),
	ValidArgs: []string{"daemon", "engine", "tui"},
}

func init() {
	logsCmd.Flags().BoolP("follow", "f", false, "Follow log output")
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

	follow, _ := cmd.Flags().GetBool("follow")
	if !follow {
		f, err := os.Open(logPath)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		defer f.Close()
		_, err = io.Copy(cmd.OutOrStdout(), f)
		return err
	}

	return followLog(cmd.Context(), logPath, cmd.OutOrStdout())
}

func followLog(ctx context.Context, path string, out io.Writer) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer w.Close()

	dir := filepath.Dir(path)
	_ = os.MkdirAll(dir, 0o700)
	if err := w.Add(dir); err != nil {
		return err
	}

	for ctx.Err() == nil {
		if err := followFile(ctx, w, path, out); err != nil {
			if !os.IsNotExist(err) {
				return err
			}
			select {
			case <-ctx.Done():
				return nil
			case <-w.Events:
			case err := <-w.Errors:
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func followFile(ctx context.Context, w *fsnotify.Watcher, path string, out io.Writer) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := w.Add(path); err != nil {
		return err
	}
	defer func() { _ = w.Remove(path) }()

	for {
		if fi, err := f.Stat(); err == nil {
			if cur, _ := f.Seek(0, io.SeekCurrent); fi.Size() < cur {
				_, _ = f.Seek(0, io.SeekStart)
			}
		}
		if _, err := io.Copy(out, f); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-w.Events:
			if !ok {
				return nil
			}
			if filepath.Clean(ev.Name) == path && (ev.Has(fsnotify.Remove) || ev.Has(fsnotify.Rename)) {
				_, _ = io.Copy(out, f)
				return nil
			}
		case err := <-w.Errors:
			if err != nil {
				return err
			}
		}
	}
}

