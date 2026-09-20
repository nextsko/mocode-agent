//go:build windows

package browser

// isRoot is always false on Windows: there is no uid, and Chromium's sandbox
// does not consult one, so --no-sandbox is never added automatically.
func isRoot() bool { return false }
