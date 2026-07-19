package version

import "runtime/debug"

// Build-time parameters set via -ldflags.

var (
	Version = "0.7.3-fromsko"
	Commit  = "unknown"
)

// A user may install Mocode using `go install github.com/nextsko/mocode-agent@latest`.
// without -ldflags, in which case the version above is unset. As a workaround
// we use the embedded build version that *is* set when using `go install` (and
// is only set for `go install` and not for `go build`).
func init() {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	mainVersion := info.Main.Version
	if mainVersion != "" && mainVersion != "(devel)" {
		Version = mainVersion
	}
}
