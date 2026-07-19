// Package errcoll provides an asynchronous error collector for tool execution
// failures and cross-platform command mistakes.
//
// Records are pushed to a buffered channel and flushed to JSONL files under the
// configured errors directory, rotated daily. The collector is project-local:
// callers pass infra.ErrorsDir() explicitly.
package errcoll

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// ErrorCategory classifies the kind of error being recorded.
type ErrorCategory string

const (
	// CategoryToolExecution captures internal tool failures (network, IO,
	// parsing, etc.).
	CategoryToolExecution ErrorCategory = "tool_execution"

	// CategoryCrossPlatform captures commands run on the wrong platform, e.g.
	// Linux tail/head/sort on Windows.
	CategoryCrossPlatform ErrorCategory = "cross_platform"

	// CategoryCommandBlocked captures commands blocked by security rules.
	CategoryCommandBlocked ErrorCategory = "command_blocked"

	// CategoryPermission captures permission-denied tool calls.
	CategoryPermission ErrorCategory = "permission"

	// CategoryProvider captures LLM provider errors (reserved entry point).
	CategoryProvider ErrorCategory = "provider"
)

// ErrorRecord is a single error event to be persisted.
type ErrorRecord struct {
	Timestamp  time.Time     `json:"timestamp"`
	SessionID  string        `json:"session_id,omitempty"`
	ToolName   string        `json:"tool_name"`
	Command    string        `json:"command,omitempty"`
	Error      string        `json:"error"`
	Category   ErrorCategory `json:"category"`
	Platform   string        `json:"platform"`
	Provider   string        `json:"provider,omitempty"`
	Model      string        `json:"model,omitempty"`
	WorkingDir string        `json:"working_dir,omitempty"`
}

// Collector buffers error records on a channel and writes them asynchronously
// to daily JSONL files.
type Collector struct {
	dir     string
	ch      chan ErrorRecord
	done    chan struct{}
	wg      sync.WaitGroup
	mu      sync.Mutex
	file    *os.File
	fileDay string
}

const (
	// channelBuffer is the number of records that can be queued before the
	// producer starts blocking.
	channelBuffer = 256
)

// New creates a Collector that writes to dir. The directory is created if it
// does not exist. Call Stop when the collector is no longer needed.
func New(dir string) (*Collector, error) {
	if dir == "" {
		return nil, errors.New("errors directory is required")
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve errors dir: %w", err)
	}

	if err := os.MkdirAll(absDir, 0o700); err != nil {
		return nil, fmt.Errorf("create errors dir: %w", err)
	}

	c := &Collector{
		dir:  absDir,
		ch:   make(chan ErrorRecord, channelBuffer),
		done: make(chan struct{}),
	}

	c.wg.Add(1)
	go c.loop()

	return c, nil
}

// Record enqueues an error record for asynchronous persistence. It never
// blocks the caller for more than the channel send. If the channel is full,
// the record is flushed synchronously to avoid losing data.
func (c *Collector) Record(rec ErrorRecord) {
	if rec.Timestamp.IsZero() {
		rec.Timestamp = time.Now()
	}
	if rec.Platform == "" {
		rec.Platform = runtime.GOOS
	}

	select {
	case c.ch <- rec:
	default:
		// Channel full: fall back to synchronous write so we don't drop
		// records under high error rates.
		c.mu.Lock()
		defer c.mu.Unlock()
		if err := c.writeLocked(rec); err != nil {
			slog.Error("Failed to write error record synchronously", "error", err)
		}
	}
}

// Stop shuts down the collector and waits for queued records to be flushed.
func (c *Collector) Stop() {
	close(c.ch)
	c.wg.Wait()

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.file != nil {
		_ = c.file.Sync()
		_ = c.file.Close()
		c.file = nil
	}
}

func (c *Collector) loop() {
	defer c.wg.Done()

	for rec := range c.ch {
		c.mu.Lock()
		err := c.writeLocked(rec)
		c.mu.Unlock()
		if err != nil {
			slog.Error("Failed to write error record", "error", err)
		}
	}
}

func (c *Collector) writeLocked(rec ErrorRecord) error {
	day := rec.Timestamp.UTC().Format("20060102")
	if c.file == nil || c.fileDay != day {
		if c.file != nil {
			_ = c.file.Sync()
			_ = c.file.Close()
		}

		path := filepath.Join(c.dir, fmt.Sprintf("errors-%s.jsonl", day))
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return fmt.Errorf("open error log %s: %w", path, err)
		}
		c.file = f
		c.fileDay = day
	}

	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal error record: %w", err)
	}

	if _, err := c.file.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write error record: %w", err)
	}

	return nil
}

// contextKey is an unexported type so no external package can collide with our
// context keys.
type contextKey struct{}

// ctxKey is the key used to store a *Collector in a context.Context.
var ctxKey = &contextKey{}

// WithContext returns a context that carries the collector.
func WithContext(ctx context.Context, c *Collector) context.Context {
	if c == nil {
		return ctx
	}
	return context.WithValue(ctx, ctxKey, c)
}

// FromContext extracts the collector from ctx. It returns nil if none is set.
func FromContext(ctx context.Context) *Collector {
	if ctx == nil {
		return nil
	}
	if c, ok := ctx.Value(ctxKey).(*Collector); ok {
		return c
	}
	return nil
}

// recordedKey marks that an error has already been recorded for the current
// tool invocation, so lower layers can avoid duplicate entries.
type recordedKey struct{}

// MarkRecorded stores a marker in ctx indicating that the current error has
// already been recorded by a higher layer.
func MarkRecorded(ctx context.Context) context.Context {
	return context.WithValue(ctx, recordedKey{}, true)
}

// IsRecorded reports whether the current context has been marked as already
// recorded.
func IsRecorded(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	recorded, _ := ctx.Value(recordedKey{}).(bool)
	return recorded
}
