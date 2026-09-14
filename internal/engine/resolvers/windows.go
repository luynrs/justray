//go:build windows

package resolvers

import (
	"net"
	"net/netip"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/luynrs/justray/internal/domain"
)

func Get() []netip.Prefix {
	var out []netip.Prefix
	seen := map[netip.Addr]bool{}
	for _, ip := range dnsServers() {
		a, ok := netip.AddrFromSlice(ip)
		if !ok {
			continue
		}
		out = add(out, seen, a)
	}
	return out
}

func dnsServers() []net.IP {
	var out []net.IP
	size := uint32(16 << 10)
	for {
		buf := make([]byte, size)
		err := windows.GetAdaptersAddresses(windows.AF_UNSPEC,
			windows.GAA_FLAG_INCLUDE_ALL_INTERFACES, 0,
			(*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0])), &size)
		switch err {
		case windows.ERROR_BUFFER_OVERFLOW:
			continue
		case nil:
		default:
			return nil
		}

		for a := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0])); a != nil; a = a.Next {
			if a.OperStatus != windows.IfOperStatusUp || windows.UTF16PtrToString(a.FriendlyName) == domain.TunInterface {
				continue
			}
			for d := a.FirstDnsServerAddress; d != nil; d = d.Next {
				if d.Address.Sockaddr == nil {
					continue
				}
				if ip := d.Address.IP(); ip != nil {
					out = append(out, ip)
				}
			}
		}
		return out
	}
}
