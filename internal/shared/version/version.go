package version

import "strings"

var Version = "dev"

func String() string {
	if Version == "dev" {
		return Version
	}
	return "v" + strings.TrimPrefix(Version, "v")
}
