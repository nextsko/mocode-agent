package version

import "runtime/debug"

// Build-time parameters set via -ldflags.

var (
	// Version is the fallback shown when no VCS/build info is available.
	// Keep it in lockstep with the latest git tag (v0.8.0) — the tag is the
	// real source of truth: init() below overrides this with the buildinfo
	// Main.Version (tag or pseudo-version), so in any git checkout this
	// string never displays.
	Version = "0.8.0"
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
