package version

import "strings"

var Version = "1.8.0-rc.4"

func String() string {
	return "v" + strings.TrimPrefix(Version, "v")
}
