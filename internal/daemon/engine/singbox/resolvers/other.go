//go:build !windows

package resolvers

import (
	"net/netip"
	"os"
	"strings"
)

func Get() []netip.Prefix {
	b, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		return nil
	}
	var out []netip.Prefix
	seen := map[netip.Addr]bool{}
	for line := range strings.Lines(string(b)) {
		f := strings.Fields(line)
		if len(f) != 2 || f[0] != "nameserver" {
			continue
		}
		a, err := netip.ParseAddr(f[1])
		if err != nil {
			continue
		}
		out = add(out, seen, a)
	}
	return out
}
