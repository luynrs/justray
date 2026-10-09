package version

import "strings"

var Version = "1.7.2-rc.2"

func String() string {
	return "v" + strings.TrimPrefix(Version, "v")
}
