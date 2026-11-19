package shell

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Session-scoped working-directory persistence for the bash tool.
//
// Every bash call used to start a fresh Shell rooted at the project
// directory, so the model had to prefix `cd <dir> &&` on nearly every
// command (446 repeated cd prefixes in a 30-day zxxw deployment trace).
// The mvdan/sh engine itself already tracks cwd across execs
// (Shell.updateShellFromRunner), so the tool layer now keeps one cwd per
// session: each call starts where the previous one ended, and the final
// directory of a command is written back after it completes.
//
// The store is process-wide and keyed by session ID, mirroring the
// singleton BackgroundShellManager the bash tool already relies on.
// Sessions never observe each other's directory; an unknown session (or
// empty session ID, e.g. some non-interactive runs) falls back to the
// project working directory passed at tool construction.

type sessionCwdStore struct {
	mu  sync.Mutex
	cwd map[string]string
}

var sessionCwd = &sessionCwdStore{cwd: make(map[string]string)}

// resolveSessionWorkingDir returns the directory a bash call should start
// in: an explicit param beats the session's persisted cwd, which beats the
// project working directory. A relative working_dir param is interpreted
// relative to the persisted cwd (or project dir) rather than the Go
// process cwd, which is what the model intends.
func resolveSessionWorkingDir(sessionID, paramDir, projectDir string) string {
	base := projectDir
	if sessionID != "" {
		sessionCwd.mu.Lock()
		if p, ok := sessionCwd.cwd[sessionID]; ok && p != "" {
			base = p
		}
		sessionCwd.mu.Unlock()
	}
	if paramDir != "" && !filepath.IsAbs(paramDir) && base != "" {
		paramDir = joinDirPath(base, paramDir)
	}
	dir := cmpOrStrings(paramDir, base)
	// A persisted (or param) directory may have been deleted since it was
	// stored; fall back to the project dir instead of failing interp setup.
	if dir != projectDir && !dirExists(dir) {
		return projectDir
	}
	return dir
}

// persistSessionWorkingDir records the end-of-run directory of a finished
// bash call. Calls with no session context (empty session ID, e.g. some
// non-interactive runs) are silently ignored.
func persistSessionWorkingDir(sessionID, dir string) {
	if sessionID == "" || dir == "" {
		return
	}
	sessionCwd.mu.Lock()
	sessionCwd.cwd[sessionID] = dir
	sessionCwd.mu.Unlock()
}

// dropSessionWorkingDir forgets a session's persisted cwd (session end
// housekeeping).
func dropSessionWorkingDir(sessionID string) {
	if sessionID == "" {
		return
	}
	sessionCwd.mu.Lock()
	delete(sessionCwd.cwd, sessionID)
	sessionCwd.mu.Unlock()
}

func cmpOrStrings(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// joinDirPath joins two path fragments without requiring the base to exist.
func joinDirPath(base, rel string) string {
	return filepath.Join(base, rel)
}

// dirExists reports whether the path is an existing directory.
func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// cleanSessionPath normalizes a path for storage and display: forward
// slashes, no trailing separator.
func cleanSessionPath(p string) string {
	return strings.TrimSuffix(filepath.ToSlash(filepath.Clean(p)), "/")
}
