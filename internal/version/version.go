package version

import "strings"

var Version = "1.5.2"

func String() string {
	return "v" + strings.TrimPrefix(Version, "v")
}
