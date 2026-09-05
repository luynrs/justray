package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/luynrs/justray/internal/version"
)

var versionCmd = &cobra.Command{
	Use:    "version",
	Hidden: true,
	Args:   cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		out(strings.TrimRight(versionBlock(), "\n"))
	},
}

func versionBlock() string {
	bin := "justray"
	if len(os.Args) > 0 && os.Args[0] != "" {
		bin = filepath.Base(os.Args[0])
	}
	ver := strings.TrimPrefix(version.String(), "v")
	p := runtime.GOOS + "/" + runtime.GOARCH
	return fmt.Sprintf("%s version %s (%s)\nhttps://github.com/luynrs/justray/releases/tag/%s\n", bin, ver, p, version.String())
}
