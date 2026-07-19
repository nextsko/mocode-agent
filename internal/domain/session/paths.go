package session

import (
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/package-register/mocode/internal/util/infra"
)

var unsafeSessionPathChars = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// DefaultStoreRoot returns the global directory used for legacy session paths.
func DefaultStoreRoot() string {
	return infra.SessionLogsDir()
}

// StoreDir returns a stable, filesystem-safe directory name for a session.
// Used by rollback snapshot storage.
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
