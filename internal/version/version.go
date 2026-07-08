// Package version resolves the running binary's version.
package version

import "runtime/debug"

// Version is injected by release builds via
// -ldflags "-X github.com/JanSuthacheeva/clickup-axi/internal/version.Version=<tag>".
// Source builds fall back to the module version that
// `go install pkg@version` embeds, then to "dev".
var Version string

func String() string {
	if Version != "" {
		return Version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}
