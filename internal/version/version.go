package version

import "strings"

var Version = "1.9.0-rc.1"

func String() string {
	return "v" + strings.TrimPrefix(Version, "v")
}
