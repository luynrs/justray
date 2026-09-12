package version

import "strings"

var Version = "1.5.0-rc2"

func String() string {
	return "v" + strings.TrimPrefix(Version, "v")
}
