// Package filetracker provides functionality to track file reads in sessions.
package filetracker

import (
	"context"
	"os"
	"path/filepath"
	"time"
)

// Service defines the interface for tracking file reads in sessions.
type Service interface {
	// RecordRead records when a file was read.
	RecordRead(ctx context.Context, sessionID, path string)

	// LastReadTime returns when a file was last read.
	// Returns zero time if never read.
	LastReadTime(ctx context.Context, sessionID, path string) time.Time

	// ListReadFiles returns the paths of all files read in a session.
	ListReadFiles(ctx context.Context, sessionID string) ([]string, error)

	// DeleteSession removes all read tracking data for a session.
	DeleteSession(ctx context.Context, sessionID string)
}

// RelPath returns the relative path from the current working directory.
func RelPath(path string) string {
	path = filepath.Clean(path)
	basepath, err := os.Getwd()
	if err != nil {
		return path
	}
	relpath, err := filepath.Rel(basepath, path)
	if err != nil {
		return path
	}
	return relpath
}
