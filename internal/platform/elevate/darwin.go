//go:build darwin

package elevate

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const helper = "/Library/PrivilegedHelperTools/justrayd"

func Needed(err error) bool {
	return err != nil && strings.Contains(err.Error(), "operation not permitted") && os.Geteuid() != 0
}

func Restart(_ string, _ *os.File) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	command := "HOME=" + shellQuote(home) + " JUSTRAY_UID=" + strconv.Itoa(os.Getuid()) + " JUSTRAY_GID=" + strconv.Itoa(os.Getgid()) + " " + shellQuote(helper)
	for _, arg := range os.Args[1:] {
		command += " " + shellQuote(arg)
	}
	command += " </dev/null >/dev/null 2>&1 &"
	if !sameHelper(self) {
		command = "/usr/bin/install -d -o root -g wheel -m 0755 /Library/PrivilegedHelperTools && " +
			"/usr/bin/install -o root -g wheel -m 0755 " + shellQuote(self) + " " + helper +
			" && { " + command + " }"
	}
	script := `do shell script "` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(command) + `" with administrator privileges`
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second-time.Second)
	defer cancel()
	process := exec.CommandContext(ctx, "osascript", "-e", script)
	process.WaitDelay = time.Second
	if output, err := process.CombinedOutput(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("start helper failed (%w, %s)", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func TakeReservation(string) (*os.File, error) { return nil, nil }

func sameHelper(source string) bool {
	sourceSum, sourceErr := hashFile(source)
	helperSum, helperErr := hashFile(helper)
	info, err := os.Stat(helper)
	if sourceErr != nil || helperErr != nil || err != nil {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == 0 && info.Mode().Perm()&0o111 != 0 && info.Mode().Perm()&0o022 == 0 && sourceSum == helperSum
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
