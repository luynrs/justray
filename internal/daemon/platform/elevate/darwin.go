//go:build darwin

package elevate

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

const helper = "/Library/PrivilegedHelperTools/justrayd"

func Needed(err error) bool {
	return err != nil && strings.Contains(err.Error(), "operation not permitted") && os.Geteuid() != 0
}

func Restart(_ string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if !sameHelper(self) {
		script := `set source to system attribute "JUSTRAY_SOURCE"
	do shell script "/usr/bin/install -o root -g wheel -m 0755 " & quoted form of source & " ` + helper + `" with administrator privileges`
		cmd := exec.Command("osascript", "-e", script)
		cmd.Env = append(os.Environ(), "JUSTRAY_SOURCE="+self)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%v: %s", err, out)
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	command := "HOME=" + shellQuote(home) + " JUSTRAY_UID=" + strconv.Itoa(os.Getuid()) + " JUSTRAY_GID=" + strconv.Itoa(os.Getgid()) + " /usr/bin/nohup " + shellQuote(helper)
	for _, arg := range os.Args[1:] {
		command += " " + shellQuote(arg)
	}
	command += " >/dev/null 2>&1 &"
	script := `do shell script "` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(command) + `" with administrator privileges`
	return exec.Command("osascript", "-e", script).Run()
}

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
