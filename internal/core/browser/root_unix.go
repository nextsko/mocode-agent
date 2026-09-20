//go:build !windows

package browser

import "os"

// isRoot reports whether the process runs as uid 0. Chromium's sandbox refuses
// to start as root (the normal case inside a container), so the launcher adds
// --no-sandbox when this is true.
func isRoot() bool { return os.Geteuid() == 0 }
