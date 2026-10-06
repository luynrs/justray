package version

import "strings"

var Version = "1.7.1"

func String() string {
	return "v" + strings.TrimPrefix(Version, "v")
}
