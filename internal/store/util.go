package store

import (
	"fmt"
	"os"
	"runtime"
	"time"
)

// readDir is a thin wrapper around os.ReadDir that returns entries sorted by name.
func readDir(dir string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	return entries, nil
}

// formatTime formats a unix timestamp as "2006-01-02".
func formatTime(unix int64) string {
	if unix <= 0 {
		return ""
	}
	return time.Unix(unix, 0).Format("2006-01-02")
}

// now returns the current unix timestamp.
func now() int64 {
	return time.Now().Unix()
}

// exists checks if a path exists.
func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// ensureDir creates a directory if it doesn't exist.
func ensureDir(path string) error {
	return os.MkdirAll(path, 0o700)
}

// removeAll removes a path and all its contents.
func removeAll(path string) error {
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("remove all %q: %w", path, err)
	}
	return nil
}

// rename atomically renames a file.
// On Windows, retries with backoff to handle file handles that haven't been
// released yet (common with queued message processing and concurrent reads).
func rename(oldPath, newPath string) error {
	err := os.Rename(oldPath, newPath)
	if err == nil {
		return nil
	}
	if runtime.GOOS != "windows" {
		return err
	}
	// On Windows, os.Rename fails with "Access is denied" if the target file
	// has an open handle from another goroutine (e.g. a concurrent ReadAll).
	// Retry with exponential backoff.
	for i := 0; i < 5; i++ {
		time.Sleep(time.Duration(50*(1<<i)) * time.Millisecond)
		err = os.Rename(oldPath, newPath)
		if err == nil {
			return nil
		}
	}
	return fmt.Errorf("rename %s -> %s: %w", oldPath, newPath, err)
}

// tempPath returns a temporary path adjacent to the given path.
func tempPath(path string) string {
	return path + ".tmp"
}

// lockPath returns the lock file path for a data file.
func lockPath(path string) string {
	return path + ".lock"
}

// openLock opens or creates a lock file.
func openLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock file %q: %w", path, err)
	}
	return f, nil
}

// closeQuietly closes a file, ignoring errors.
func closeQuietly(f *os.File) {
	_ = f.Close()
}

// removeQuietly removes a file, ignoring errors.
func removeQuietly(path string) {
	_ = os.Remove(path)
}

// parseInt64 parses a string to int64, returning 0 on error.
func parseInt64(s string) int64 {
	var v int64
	_, _ = fmt.Sscanf(s, "%d", &v)
	return v
}
