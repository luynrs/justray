package version

import "strings"

var Version = "1.9.0"

func String() string {
	return "v" + strings.TrimPrefix(Version, "v")
}
