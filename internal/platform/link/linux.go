//go:build linux

package link

import "github.com/sagernet/netlink"

func Delete(iface string) {
	if l, err := netlink.LinkByName(iface); err == nil {
		_ = netlink.LinkDel(l)
	}
}
