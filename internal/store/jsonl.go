package store

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// JSONLWriter provides atomic append operations to a JSONL file.
// It uses an external lock file to guarantee cross-process mutual exclusion,
// combined with append+fsync for durability.
// For in-process concurrency, it uses sync.RWMutex to allow concurrent reads
// while blocking writes.
type JSONLWriter struct {
	path string
	mu   sync.RWMutex
}

// NewJSONLWriter creates a writer for the given JSONL file path.
func NewJSONLWriter(path string) *JSONLWriter {
	return &JSONLWriter{path: path}
}

// Path returns the JSONL file path.
func (w *JSONLWriter) Path() string { return w.path }

// Append atomically appends a single value as a JSON line to the file.
// It acquires an exclusive file lock on a sidecar .lock file before writing,
// then seeks to end, encodes the value, and fsyncs.
func (w *JSONLWriter) Append(value any) error {
	// Acquire exclusive in-process lock
	w.mu.Lock()
	defer w.mu.Unlock()

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(w.path), 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(w.path), err)
	}

	// Acquire lock on sidecar lock file
	lockPath := w.path + ".lock"
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open lock file %s: %w", lockPath, err)
	}
	defer lockFile.Close()

	if err := LockFile(lockFile); err != nil {
		return fmt.Errorf("lock %s: %w", lockPath, err)
	}
	defer func() { _ = UnlockFile(lockFile) }()

	// Open the JSONL file for append
	f, err := os.OpenFile(w.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open %s: %w", w.path, err)
	}
	defer f.Close()

	// Seek to end to guarantee we're at the real end (in case another
	// process appended after we acquired the lock — the lock is on a
	// sidecar, not on the data file itself).
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return fmt.Errorf("seek end %s: %w", w.path, err)
	}

	// Encode the value as a single JSON line
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("write %s: %w", w.path, err)
	}
	if _, err := f.Write([]byte{'\n'}); err != nil {
		return fmt.Errorf("write newline %s: %w", w.path, err)
	}

	// Fsync for durability
	if err := f.Sync(); err != nil {
		return fmt.Errorf("fsync %s: %w", w.path, err)
	}

	return nil
}

// ReadAll reads all values from the JSONL file, decoding each line with
// the given new-value factory function.
// Uses a shared lock to allow concurrent reads while blocking writes.
func (w *JSONLWriter) ReadAll(newValue func() any) ([]any, error) {
	// Acquire read lock for in-process concurrency
	w.mu.RLock()
	defer w.mu.RUnlock()

	// Acquire shared lock on sidecar lock file
	lockPath := w.path + ".lock"
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock file %s: %w", lockPath, err)
	}
	defer lockFile.Close()

	if err := LockFileShared(lockFile); err != nil {
		return nil, fmt.Errorf("shared lock %s: %w", lockPath, err)
	}
	defer func() { _ = UnlockFile(lockFile) }()

	f, err := os.Open(w.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open %s: %w", w.path, err)
	}
	defer f.Close()

	var results []any
	scanner := bufio.NewScanner(f)
	// Increase buffer for large lines (messages with binary parts can be large)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		v := newValue()
		if err := json.Unmarshal([]byte(line), v); err != nil {
			return nil, fmt.Errorf("unmarshal line: %w", err)
		}
		results = append(results, v)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", w.path, err)
	}

	return results, nil
}

// ScanLines calls fn for each line in the JSONL file.
// Returns early if fn returns an error.
// Uses a shared lock to allow concurrent reads while blocking writes.
func (w *JSONLWriter) ScanLines(fn func(line string) error) error {
	// Acquire read lock for in-process concurrency
	w.mu.RLock()
	defer w.mu.RUnlock()

	// Acquire shared lock on sidecar lock file
	lockPath := w.path + ".lock"
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open lock file %s: %w", lockPath, err)
	}
	defer lockFile.Close()

	if err := LockFileShared(lockFile); err != nil {
		return fmt.Errorf("shared lock %s: %w", lockPath, err)
	}
	defer func() { _ = UnlockFile(lockFile) }()

	f, err := os.Open(w.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("open %s: %w", w.path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		if err := fn(line); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan %s: %w", w.path, err)
	}
	return nil
}

// decodeJSONLine unmarshals one JSONL line into v. It reports false for lines
// that are not valid JSON; callers skip those so a single corrupt record
// cannot hide the rest of the file.
func decodeJSONLine(line string, v any) bool {
	return json.Unmarshal([]byte(line), v) == nil
}

// WriteAll atomically replaces the entire file content.
// Uses temp file + rename for atomicity.
func (w *JSONLWriter) WriteAll(values []any) error {
	// Acquire exclusive in-process lock
	w.mu.Lock()
	defer w.mu.Unlock()

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(w.path), 0o700); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	lockPath := w.path + ".lock"
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open lock file: %w", err)
	}
	defer lockFile.Close()

	if err := LockFile(lockFile); err != nil {
		return fmt.Errorf("lock: %w", err)
	}
	defer func() { _ = UnlockFile(lockFile) }()

	tmpPath := w.path + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open temp: %w", err)
	}

	enc := json.NewEncoder(f)
	for _, v := range values {
		if err := enc.Encode(v); err != nil {
			f.Close()
			_ = os.Remove(tmpPath)
			return fmt.Errorf("encode: %w", err)
		}
	}
	if err := f.Sync(); err != nil {
		f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("fsync: %w", err)
	}
	f.Close()

	if err := rename(tmpPath, w.path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename: %w", err)
	}

	return nil
}

// Exists returns true if the JSONL file exists.
func (w *JSONLWriter) Exists() bool {
	_, err := os.Stat(w.path)
	return err == nil
}

// Remove deletes the JSONL file and its lock file.
func (w *JSONLWriter) Remove() error {
	_ = os.Remove(w.path + ".lock")
	err := os.Remove(w.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("remove %s: %w", w.path, err)
	}
	return nil
}

// WriteJSON atomic writes a JSON value to a file.
func WriteJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	tmpPath := path + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open %s: %w", tmpPath, err)
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(value); err != nil {
		f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("encode: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("fsync: %w", err)
	}
	f.Close()
	if err := rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// ReadJSON reads a JSON file into the given value.
func ReadJSON(path string, v any) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return err
		}
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	if err := json.NewDecoder(f).Decode(v); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}
