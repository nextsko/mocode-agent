package log

import (
	"path/filepath"
	"regexp"
)

var safeNameRegexp = regexp.MustCompile(`[^a-zA-Z0-9._-]`)

const (
	// LogsDirName is the subdirectory under the data directory for log files.
	LogsDirName = "logs"
	// MainLogFileName is the canonical primary log file name (lowercase).
	MainLogFileName = "mocode.log"
)

// MainLogPath returns the canonical path to the primary application log file.
func MainLogPath(dataDirectory string) string {
	return filepath.Join(dataDirectory, LogsDirName, MainLogFileName)
}

// ServerLogPath returns the log path for a named server instance under the cache dir.
func ServerLogPath(cacheDir, serverHost string) string {
	safe := safeNameRegexp.ReplaceAllString(serverHost, "_")
	return filepath.Join(cacheDir, "server-"+safe, MainLogFileName)
}
