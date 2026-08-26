// Package version carries the release version, overridden by the linker on release builds
package version

import "strings"

var Version = "1.0.0"

func String() string { return "v" + strings.TrimPrefix(Version, "v") }
