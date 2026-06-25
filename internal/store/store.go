// Package store provides file-based persistence for mocode's domain data:
// sessions, messages, file history, file tracker, and memories.
//
// This replaces the SQLite-backed internal/db/ package entirely.
// All data is stored as JSONL files with sidecar indexes under a
// centralized directory (~/.local/share/mocode/ or %LOCALAPPDATA%/mocode/).
package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/zeebo/xxh3"

	"github.com/package-register/mocode/internal/core/config"
	"github.com/package-register/mocode/internal/domain/session"
)

// Store is the top-level file-based persistence layer.
// It owns the project directory and all sub-stores.
type Store struct {
	// DataDir is the global root: ~/.local/share/mocode/ on Linux,
	// %LOCALAPPDATA%/mocode/ on Windows.
	DataDir string

	// ProjectPath is the absolute working directory of the project.
	ProjectPath string

	// ProjectDir is the per-project data directory:
	// <DataDir>/projects/<name>_<hash8>/
	ProjectDir string

	sessions *SessionStore
	messages *MessageStore
	files    *FileHistoryStore
	tracker  *FileTrackerStore
	memories *MemoryStore
	stats    *StatsEngine

	search *SessionSearch
	mu     sync.RWMutex
}

// New creates a new Store for the given project path.
// cfg is used to locate the global data directory.
func New(projectPath string, cfg *config.ConfigStore) (*Store, error) {
	if projectPath == "" {
		return nil, fmt.Errorf("projectPath is required")
	}
	absPath, err := filepath.Abs(projectPath)
	if err != nil {
		return nil, fmt.Errorf("resolve project path: %w", err)
	}

	dataDir := globalStoreDir(cfg)
	projDir := projectDirPath(dataDir, absPath)

	if err := os.MkdirAll(projDir, 0o700); err != nil {
		return nil, fmt.Errorf("create project dir %s: %w", projDir, err)
	}

	s := &Store{
		DataDir:     dataDir,
		ProjectPath: absPath,
		ProjectDir:  projDir,
	}

	s.sessions = newSessionStore(s)
	s.messages = newMessageStore(s)
	s.files = newFileHistoryStore(s)
	s.tracker = newFileTrackerStore(s)
	s.memories = newMemoryStore(s)
	s.stats = &StatsEngine{store: s}

	s.search = newSessionSearch(s)

	return s, nil
}

// Sessions returns the session store.
func (s *Store) Sessions() *SessionStore { return s.sessions }

// Messages returns the message store.
func (s *Store) Messages() *MessageStore { return s.messages }

// Files returns the file history store.
func (s *Store) Files() *FileHistoryStore { return s.files }

// Tracker returns the file tracker store.
func (s *Store) Tracker() *FileTrackerStore { return s.tracker }

// Memories returns the memory store.
func (s *Store) Memories() *MemoryStore { return s.memories }

// Stats returns the stats engine.
func (s *Store) Stats() *StatsEngine { return s.stats }

// Search returns the session search engine.
func (s *Store) Search() *SessionSearch { return s.search }

// Close releases any resources held by the store.
func (s *Store) Close() error {
	if s.search != nil {
		_ = s.search.Close()
	}
	return nil
}

// sessionDir returns the directory for a specific session.
func (s *Store) sessionDir(sess session.Session) string {
	return filepath.Join(s.ProjectDir, "sessions", sessionDirName(sess))
}

// sessionsDir returns the root sessions directory for the project.
func (s *Store) sessionsDir() string {
	return filepath.Join(s.ProjectDir, "sessions")
}

// sessionsIndexPath returns the path to the sessions index file.
func (s *Store) sessionsIndexPath() string {
	return filepath.Join(s.ProjectDir, "sessions_index.json")
}

// globalStoreDir returns the global data directory where all projects live.
func globalStoreDir(cfg *config.ConfigStore) string {
	// Use the same directory that holds the global mocode.json
	return filepath.Dir(config.GlobalConfigData())
}

// projectDirName returns the directory-safe project identifier.
// Format: <base>_<xxh3-hex-prefix8>
func projectDirName(projectPath string) string {
	base := filepath.Base(projectPath)
	base = sanitize(base)
	hash := xxh3.HashString(filepath.Clean(projectPath))
	return fmt.Sprintf("%s_%x", base, hash)[:len(base)+1+8] // name + _ + 8 hex chars
}

// projectDirPath returns the full path for a project's data directory.
func projectDirPath(dataDir, projectPath string) string {
	return filepath.Join(dataDir, "projects", projectDirName(projectPath))
}

// sessionDirName returns the directory name for a session.
// Format: <created_at>_<id-hash12>
// Using a hash of the full ID (rather than just a prefix) ensures uniqueness
// even when multiple sessions share ID prefixes (e.g., concurrent sub-agents
// created from the same parent tool call).
func sessionDirName(sess session.Session) string {
	return fmt.Sprintf("%d_%x", sess.CreatedAt, xxh3.HashString(sess.ID))
}

// sanitize replaces characters unsafe for directory names.
// Works on runes for correct multi-byte handling.
func sanitize(name string) string {
	b := make([]byte, 0, len(name))
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b = append(b, string(r)...)
		} else {
			b = append(b, '-')
		}
	}
	if len(b) == 0 {
		return "project"
	}
	return string(b)
}

// EnsureDir creates a directory if it doesn't exist.
func (s *Store) EnsureDir(path string) error {
	return os.MkdirAll(path, 0o700)
}

// EnsureDirContext creates a directory, respecting context cancellation.
func (s *Store) EnsureDirContext(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.MkdirAll(path, 0o700)
}
