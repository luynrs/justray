package version

import "strings"

var Version = "1.6.5"

func String() string {
	return "v" + strings.TrimPrefix(Version, "v")
}
