//go:build linux

package link

import "os/exec"

func Delete(iface string) {
	_ = exec.Command("ip", "link", "del", iface).Run()
}
