package memory

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Store is a file-backed memory tree.
//
// Every write is a temp-file-plus-rename into the destination directory, so a
// crash or a validation failure can never leave a truncated entry behind. There
// is no lock file: entries are independent files, writes are atomic per file,
// and the index is derived by scanning — which makes a lost index update
// self-healing on the next rebuild rather than a corruption to repair.
type Store struct {
	root string
	now  func() time.Time
}

// NewStore returns a store rooted at dir.
func NewStore(dir string) *Store {
	return &Store{root: dir, now: func() time.Time { return time.Now().UTC() }}
}

// WithClock overrides the clock, for tests.
func (s *Store) WithClock(now func() time.Time) *Store {
	s.now = now
	return s
}

// Root is the absolute memory directory.
func (s *Store) Root() string { return s.root }

// WriteResult describes a successful write.
type WriteResult struct {
	// Path is the absolute file path.
	Path string
	// RelPath is the path relative to the root, which is how the entry is
	// addressed elsewhere (URI namespace, index, extraction).
	RelPath string
	// Overwrote reports whether an entry already existed at that path.
	Overwrote bool
}

// Write stores an entry, filling in its date, path and relative path.
//
// The target path is derived, not supplied: an entry's identity is
// (scope, session, timestamp, slug), so a caller cannot accidentally write the
// same memory into two different places or overwrite an unrelated one.
func (s *Store) Write(_ context.Context, e *Entry) (WriteResult, error) {
	if e == nil {
		return WriteResult{}, fmt.Errorf("%w: nil entry", ErrInvalidEntry)
	}
	if e.Date.IsZero() {
		if e.Date = s.now(); e.Date.IsZero() {
			e.Date = time.Now().UTC()
		}
	}
	if err := e.Validate(); err != nil {
		return WriteResult{}, err
	}
	e.Tags = normalizeTags(e.Tags)

	relDir := e.RelDir()
	relPath := path.Join(relDir, e.FileName())

	absDir := filepath.Join(s.root, filepath.FromSlash(relDir))
	absPath := filepath.Join(s.root, filepath.FromSlash(relPath))

	if err := s.checkInsideRoot(absPath); err != nil {
		return WriteResult{}, err
	}

	data, err := e.Render()
	if err != nil {
		return WriteResult{}, err
	}

	// Round-trip guard: never persist bytes we cannot read back.
	if _, err := ParseEntry(data); err != nil {
		return WriteResult{}, fmt.Errorf("rendered memory failed to parse: %w", err)
	}

	_, statErr := os.Stat(absPath)
	overwrote := statErr == nil

	if err := writeFileAtomic(absDir, absPath, data); err != nil {
		return WriteResult{}, err
	}

	e.Path = absPath
	e.RelPath = relPath
	return WriteResult{Path: absPath, RelPath: relPath, Overwrote: overwrote}, nil
}

// Read loads the entry at a root-relative path.
func (s *Store) Read(relPath string) (*Entry, error) {
	absPath, err := s.absPath(relPath)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, relPath)
		}
		return nil, fmt.Errorf("reading %s: %w", relPath, err)
	}

	e, err := ParseEntry(data)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", relPath, err)
	}
	e.Path = absPath
	e.RelPath = strings.TrimPrefix(filepath.ToSlash(relPath), "/")
	return e, nil
}

// ListOptions filters a listing.
type ListOptions struct {
	// Scope restricts to one tier. Empty means both.
	Scope Scope
	// Session restricts session entries to one session. Ignored for globals.
	Session string
	// Limit caps the result. Zero means unlimited.
	Limit int
}

// List returns entries, newest first.
//
// An unreadable or malformed file is skipped rather than failing the listing:
// the tree is user-editable, so one bad hand-written file must not make the
// whole memory unreadable.
func (s *Store) List(_ context.Context, opts ListOptions) ([]*Entry, error) {
	var out []*Entry

	scopes := ValidScopes()
	if opts.Scope != "" {
		if !opts.Scope.Valid() {
			return nil, fmt.Errorf("%w: scope must be one of %s", ErrInvalidEntry, scopeList())
		}
		scopes = []Scope{opts.Scope}
	}

	for _, scope := range scopes {
		dirs, err := s.scopeDirs(scope, opts.Session)
		if err != nil {
			return nil, err
		}
		for _, relDir := range dirs {
			entries, err := s.readDirEntries(relDir)
			if err != nil {
				return nil, err
			}
			out = append(out, entries...)
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Date.Equal(out[j].Date) {
			return out[i].Date.After(out[j].Date)
		}
		return out[i].RelPath < out[j].RelPath
	})

	if opts.Limit > 0 && len(out) > opts.Limit {
		out = out[:opts.Limit]
	}
	if out == nil {
		out = []*Entry{}
	}
	return out, nil
}

// scopeDirs returns the root-relative directories a scope occupies.
func (s *Store) scopeDirs(scope Scope, session string) ([]string, error) {
	switch scope {
	case ScopeGlobal:
		return []string{GlobalDir}, nil
	case ScopeSession:
		if session != "" {
			short := sessionShort(session)
			if short == "" {
				return nil, fmt.Errorf("%w: unusable session id %q", ErrInvalidEntry, session)
			}
			return []string{path.Join(SessionsDir, short)}, nil
		}
		entries, err := os.ReadDir(filepath.Join(s.root, SessionsDir))
		if err != nil {
			if os.IsNotExist(err) {
				return nil, nil
			}
			return nil, fmt.Errorf("listing sessions: %w", err)
		}
		dirs := make([]string, 0, len(entries))
		for _, e := range entries {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				dirs = append(dirs, path.Join(SessionsDir, e.Name()))
			}
		}
		return dirs, nil
	default:
		return nil, fmt.Errorf("%w: unknown scope %q", ErrInvalidEntry, scope)
	}
}

// readDirEntries loads every entry in one root-relative directory.
func (s *Store) readDirEntries(relDir string) ([]*Entry, error) {
	absDir := filepath.Join(s.root, filepath.FromSlash(relDir))
	dirEntries, err := os.ReadDir(absDir)
	if err != nil {
		if os.IsNotExist(err) {
			// A tier with nothing in it yet is normal, not an error.
			return nil, nil
		}
		return nil, fmt.Errorf("listing %s: %w", relDir, err)
	}

	var out []*Entry
	for _, de := range dirEntries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".md") || de.Name() == IndexFileName {
			continue
		}
		relPath := path.Join(relDir, de.Name())

		e, err := s.Read(relPath)
		if err != nil {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

// Forget moves an entry into the archive rather than deleting it.
//
// Memory that can be destroyed by one mistaken call is memory an agent will not
// risk writing, so removal is always recoverable. The archive is a dotted
// directory, which keeps it out of listings and out of the URI namespace's
// browsable view.
func (s *Store) Forget(_ context.Context, relPath string) (string, error) {
	absPath, err := s.absPath(relPath)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(absPath); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("%w: %s", ErrNotFound, relPath)
		}
		return "", fmt.Errorf("reading %s: %w", relPath, err)
	}

	archiveDir := filepath.Join(s.root, ArchiveDir)
	archived := filepath.Join(archiveDir, s.now().UTC().Format("20060102T150405")+"-"+filepath.Base(absPath))

	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return "", fmt.Errorf("creating archive directory: %w", err)
	}
	if err := os.Rename(absPath, archived); err != nil {
		return "", fmt.Errorf("archiving %s: %w", relPath, err)
	}
	return filepath.ToSlash(filepath.Join(ArchiveDir, filepath.Base(archived))), nil
}

// Match is a search hit with the reason it matched.
type Match struct {
	Entry *Entry
	// Where records the strongest field that matched: "title", "tag" or "body".
	Where string
	// Snippet is a short excerpt around a body match.
	Snippet string
}

// Search finds entries whose title, tags or body contain query, ranked by where
// the match was found.
//
// Ranking by field rather than by a score is deliberate: a substring search has
// no meaningful relevance metric, and pretending otherwise would bury a title
// match under a long body that happens to mention the term more often.
func (s *Store) Search(_ context.Context, query string, opts ListOptions) ([]Match, error) {
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return nil, fmt.Errorf("%w: query is required", ErrInvalidEntry)
	}

	entries, err := s.List(context.Background(), opts)
	if err != nil {
		return nil, err
	}

	var matches []Match
	for _, e := range entries {
		switch {
		case strings.Contains(strings.ToLower(e.Title), needle):
			matches = append(matches, Match{Entry: e, Where: "title"})
		case containsTag(e.Tags, needle):
			matches = append(matches, Match{Entry: e, Where: "tag"})
		case strings.Contains(strings.ToLower(e.Body), needle):
			matches = append(matches, Match{Entry: e, Where: "body", Snippet: snippet(e.Body, needle)})
		}
	}

	// Stable ordering by rank, then by date (already newest-first within a rank).
	rank := map[string]int{"title": 0, "tag": 1, "body": 2}
	sort.SliceStable(matches, func(i, j int) bool {
		return rank[matches[i].Where] < rank[matches[j].Where]
	})
	return matches, nil
}

func containsTag(tags []string, needle string) bool {
	for _, t := range tags {
		if strings.Contains(strings.ToLower(t), needle) {
			return true
		}
	}
	return false
}

// snippet returns a short window of body around the first match.
func snippet(body, needle string) string {
	lower := strings.ToLower(body)
	i := strings.Index(lower, needle)
	if i < 0 {
		return ""
	}

	const window = 80
	start := max(0, i-window/2)
	end := min(len(body), i+len(needle)+window/2)

	out := strings.TrimSpace(body[start:end])
	out = strings.Join(strings.Fields(out), " ")
	if start > 0 {
		out = "…" + out
	}
	if end < len(body) {
		out += "…"
	}
	return out
}

// absPath resolves a root-relative path and refuses anything that escapes.
func (s *Store) absPath(relPath string) (string, error) {
	clean := strings.Trim(strings.TrimSpace(relPath), "/")
	if clean == "" {
		return "", fmt.Errorf("%w: empty path", ErrUnsafePath)
	}
	if strings.ContainsRune(clean, '\\') {
		return "", fmt.Errorf("%w: use forward slashes", ErrUnsafePath)
	}
	for _, seg := range strings.Split(clean, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", fmt.Errorf("%w: %s", ErrUnsafePath, relPath)
		}
	}

	absPath := filepath.Join(s.root, filepath.FromSlash(clean))
	if err := s.checkInsideRoot(absPath); err != nil {
		return "", err
	}
	return absPath, nil
}

// checkInsideRoot asserts a resolved absolute path is inside the memory root.
func (s *Store) checkInsideRoot(absPath string) error {
	root, err := filepath.Abs(s.root)
	if err != nil {
		return fmt.Errorf("resolving memory root: %w", err)
	}
	target, err := filepath.Abs(absPath)
	if err != nil {
		return fmt.Errorf("resolving %s: %w", absPath, err)
	}

	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%w: %s escapes the memory directory", ErrUnsafePath, absPath)
	}
	return nil
}

// writeFileAtomic writes data into dir and renames it over target.
func writeFileAtomic(dir, target string, data []byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".memory-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	// No-op once the rename below has moved it.
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", tmpName, err)
	}
	if err := renameOver(tmpName, target); err != nil {
		return fmt.Errorf("replacing %s: %w", target, err)
	}
	return nil
}

// renameOver renames, retrying briefly on Windows where a just-closed handle
// can still be held by the OS or an indexer.
func renameOver(from, to string) error {
	var err error
	for attempt := range 5 {
		if err = os.Rename(from, to); err == nil {
			return nil
		}
		if !errors.Is(err, os.ErrPermission) {
			return err
		}
		time.Sleep(time.Duration(10*(attempt+1)) * time.Millisecond)
	}
	return err
}
