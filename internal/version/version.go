package version

import "strings"

var Version = "1.7.0-alpha.3"

func String() string {
	return "v" + strings.TrimPrefix(Version, "v")
}
