//go:build windows

package device

import (
	"context"
	"fmt"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func Info(context.Context) (osName, hwid, ver, model string) {
	reg := func(path, name string) (v string) {
		if k, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.QUERY_VALUE); err == nil {
			v, _, _ = k.GetStringValue(name)
			k.Close()
		}
		return
	}
	v := windows.RtlGetVersion()
	return "Windows", reg(`SOFTWARE\Microsoft\Cryptography`, "MachineGuid"),
		fmt.Sprintf("%d.%d.%d", v.MajorVersion, v.MinorVersion, v.BuildNumber),
		reg(`HARDWARE\DESCRIPTION\System\BIOS`, "SystemProductName")
}
