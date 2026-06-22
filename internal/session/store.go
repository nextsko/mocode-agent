package session

import (
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/package-register/mocode/internal/infra"
)

var unsafeSessionPathChars = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// DefaultStoreRoot returns the global directory used for session databases.
func DefaultStoreRoot() string {
	return filepath.Join(infra.Dir(), ".mocode", "sessions")
}

// DBPath returns the database path for a session-specific store directory.
func DBPath(storeDir string) string {
	return filepath.Join(storeDir, "mocode.db")
}

// StoreDir returns a stable, filesystem-safe directory for a session.
func StoreDir(root string, s Session) string {
	name := strings.Trim(unsafeSessionPathChars.ReplaceAllString(s.ID, "-"), "-._")
	if name == "" {
		name = "session"
	}
	if s.CreatedAt > 0 {
		name += "_" + time.Unix(s.CreatedAt, 0).Format("20060102-150405")
	}
	return filepath.Join(root, name)
}

func StoreDirForID(root, id string) string {
	name := strings.Trim(unsafeSessionPathChars.ReplaceAllString(id, "-"), "-._")
	if name == "" {
		name = "session"
	}
	matches, _ := filepath.Glob(filepath.Join(root, name+"*"))
	if len(matches) > 0 {
		return matches[0]
	}
	return filepath.Join(root, name)
}

func StoreDBPathForID(root, id string) string {
	return DBPath(StoreDirForID(root, id))
}
