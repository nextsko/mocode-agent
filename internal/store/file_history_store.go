package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"github.com/nextsko/mocode-agent/internal/domain/history"
)

// FileHistoryStore provides file-based file version history persistence.
type FileHistoryStore struct {
	store *Store
}

func newFileHistoryStore(s *Store) *FileHistoryStore {
	return &FileHistoryStore{store: s}
}

// jsonlFile is the on-disk representation of a file version.
type jsonlFile struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	Path      string `json:"path"`
	Content   string `json:"content"`
	Version   int64  `json:"version"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

func (fhs *FileHistoryStore) filesPath(sessionID string) string {
	if meta, ok := fhs.store.Sessions().index.Sessions[sessionID]; ok {
		dir := fhs.store.sessionDir(sessionMetaToSession(*meta))
		return filepath.Join(dir, "files.jsonl")
	}
	return filepath.Join(fhs.store.ProjectDir, "sessions", sessionID, "files.jsonl")
}

func (fhs *FileHistoryStore) sessionDirPath(sessionID string) string {
	if meta, ok := fhs.store.Sessions().index.Sessions[sessionID]; ok {
		return fhs.store.sessionDir(sessionMetaToSession(*meta))
	}
	return filepath.Join(fhs.store.ProjectDir, "sessions", sessionID)
}

// Create records the first version of a file.
func (fhs *FileHistoryStore) Create(ctx context.Context, sessionID, path, content string) (history.File, error) {
	return fhs.CreateVersion(ctx, sessionID, path, content)
}

// CreateVersion records a new version of a file.
func (fhs *FileHistoryStore) CreateVersion(ctx context.Context, sessionID, filePath, content string) (history.File, error) {
	now := time.Now().Unix()

	// Determine next version number
	var latestVersion int64
	fhs.scanFiles(sessionID, func(jf jsonlFile) {
		if jf.Path == filePath && jf.Version > latestVersion {
			latestVersion = jf.Version
		}
	})

	f := history.File{
		ID:        uuid.New().String(),
		SessionID: sessionID,
		Path:      filePath,
		Content:   content,
		Version:   latestVersion + 1,
		CreatedAt: now,
		UpdatedAt: now,
	}

	jf := jsonlFile{
		ID:        f.ID,
		SessionID: f.SessionID,
		Path:      f.Path,
		Content:   f.Content,
		Version:   f.Version,
		CreatedAt: f.CreatedAt,
		UpdatedAt: f.UpdatedAt,
	}

	writer := NewJSONLWriter(fhs.filesPath(sessionID))
	if err := writer.Append(jf); err != nil {
		return history.File{}, err
	}
	return f, nil
}

// Get retrieves a file by ID.
func (fhs *FileHistoryStore) Get(ctx context.Context, id string) (history.File, error) {
	var result *history.File
	found := false
	fhs.forEachSession(func(sessionID string) bool {
		fhs.scanFiles(sessionID, func(jf jsonlFile) {
			if jf.ID == id {
				f := jsonlToFile(jf)
				result = &f
				found = true
			}
		})
		return !found // stop if found
	})
	if result == nil {
		return history.File{}, fmt.Errorf("file not found: %s", id)
	}
	return *result, nil
}

// GetByPathAndSession returns the latest version of a file in a session.
func (fhs *FileHistoryStore) GetByPathAndSession(ctx context.Context, filePath, sessionID string) (history.File, error) {
	var latest *jsonlFile
	fhs.scanFiles(sessionID, func(jf jsonlFile) {
		if jf.Path == filePath {
			if latest == nil || jf.Version > latest.Version {
				c := jf
				latest = &c
			}
		}
	})
	if latest == nil {
		return history.File{}, fmt.Errorf("file not found: %s in session %s", filePath, sessionID)
	}
	return jsonlToFile(*latest), nil
}

// ListBySession returns all file versions in a session.
func (fhs *FileHistoryStore) ListBySession(ctx context.Context, sessionID string) ([]history.File, error) {
	var files []history.File
	fhs.scanFiles(sessionID, func(jf jsonlFile) {
		files = append(files, jsonlToFile(jf))
	})
	return files, nil
}

// ListLatestSessionFiles returns only the latest version of each file in a session.
func (fhs *FileHistoryStore) ListLatestSessionFiles(ctx context.Context, sessionID string) ([]history.File, error) {
	latest := make(map[string]jsonlFile)
	fhs.scanFiles(sessionID, func(jf jsonlFile) {
		if existing, ok := latest[jf.Path]; !ok || jf.Version > existing.Version {
			latest[jf.Path] = jf
		}
	})
	var files []history.File
	for _, jf := range latest {
		files = append(files, jsonlToFile(jf))
	}
	return files, nil
}

// Delete removes a specific file version.
func (fhs *FileHistoryStore) Delete(ctx context.Context, id string) error {
	// Find which session this file belongs to
	var targetSessionID string
	found := false
	fhs.forEachSession(func(sessionID string) bool {
		fhs.scanFiles(sessionID, func(jf jsonlFile) {
			if jf.ID == id {
				targetSessionID = sessionID
				found = true
			}
		})
		return !found
	})
	if !found {
		return fmt.Errorf("file not found: %s", id)
	}

	// Rewrite the file omitting the target
	writer := NewJSONLWriter(fhs.filesPath(targetSessionID))
	all, err := writer.ReadAll(func() any { return &jsonlFile{} })
	if err != nil {
		return err
	}
	var newValues []any
	for _, v := range all {
		jf, ok := v.(*jsonlFile)
		if !ok {
			continue
		}
		if jf.ID != id {
			newValues = append(newValues, jf)
		}
	}
	return writer.WriteAll(newValues)
}

// DeleteSessionFiles removes all file versions for a session.
func (fhs *FileHistoryStore) DeleteSessionFiles(ctx context.Context, sessionID string) error {
	return NewJSONLWriter(fhs.filesPath(sessionID)).Remove()
}

// scanFiles reads all file versions from a session's JSONL and calls fn for each.
func (fhs *FileHistoryStore) scanFiles(sessionID string, fn func(jsonlFile)) {
	writer := NewJSONLWriter(fhs.filesPath(sessionID))
	if !writer.Exists() {
		return
	}
	_ = writer.ScanLines(func(line string) error {
		var jf jsonlFile
		if err := json.Unmarshal([]byte(line), &jf); err != nil {
			return nil
		}
		fn(jf)
		return nil
	})
}

// forEachSession iterates over all session directories.
func (fhs *FileHistoryStore) forEachSession(fn func(sessionID string) bool) {
	dir := filepath.Join(fhs.store.ProjectDir, "sessions")
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if !fn(entry.Name()) {
			return
		}
	}
}

func jsonlToFile(jf jsonlFile) history.File {
	return history.File{
		ID:        jf.ID,
		SessionID: jf.SessionID,
		Path:      jf.Path,
		Content:   jf.Content,
		Version:   jf.Version,
		CreatedAt: jf.CreatedAt,
		UpdatedAt: jf.UpdatedAt,
	}
}
