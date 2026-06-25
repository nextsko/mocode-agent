package log

import "path/filepath"

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
