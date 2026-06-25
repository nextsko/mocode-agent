// Package sessionlog provides structured per-session logging for the
// self-evolving agent system. Each session writes categorized log files
// into a session directory, enabling post-hoc analysis by the evolution agent.
//
// Directory structure (under .mocode/sessions/<session-id>/):
//
//	bug.md       - errors, failures, exceptions encountered
//	info.md      - decisions, milestones, key findings
//	runtime.md   - execution state transitions, agent switches
//	toolcall.md  - tool invocations, parameters, results, timing
//	thinks.md    - reasoning traces from model responses
//	user.md      - user prompts and interactions
//	todo.md      - task tracking throughout session
//	changes.md   - file modifications, git activity
//	summary.md   - auto-generated session summary on close
package sessionlog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ── Entry types ─────────────────────────────────────────────────────────────

// Entry is a single log event.
type Entry struct {
	Timestamp time.Time `json:"timestamp"`
	Category  string    `json:"category"` // bug, info, runtime, toolcall, thinks, user, todo, changes
	Event     string    `json:"event"`    // short event name (e.g. "tool_call_start", "error_occurred")
	Data      string    `json:"data"`     // structured or freeform content
	Meta      Meta      `json:"meta,omitempty"`
}

// Meta holds optional structured metadata for an entry.
type Meta struct {
	ToolName    string `json:"tool_name,omitempty"`
	ToolCallID  string `json:"tool_call_id,omitempty"`
	AgentID     string `json:"agent_id,omitempty"`
	DurationMs  int64  `json:"duration_ms,omitempty"`
	ErrorType   string `json:"error_type,omitempty"`
	FileChanged string `json:"file_changed,omitempty"`
}

// ── Logger ──────────────────────────────────────────────────────────────────

// Logger writes structured session logs.
type Logger struct {
	mu        sync.Mutex
	dir       string
	sessionID string

	files map[string]*os.File // category -> file handle
}

// NewLogger creates a logger for the given session.
// baseDir is typically .mocode/sessions/
func NewLogger(baseDir, sessionID string) (*Logger, error) {
	dir := filepath.Join(baseDir, sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create session log dir: %w", err)
	}
	return &Logger{
		dir:       dir,
		sessionID: sessionID,
		files:     make(map[string]*os.File),
	}, nil
}

// Log writes an entry to the appropriate category file.
func (l *Logger) Log(e Entry) {
	l.mu.Lock()
	defer l.mu.Unlock()

	e.Timestamp = time.Now()
	if e.Category == "" {
		e.Category = "info"
	}

	f, err := l.getFile(e.Category)
	if err != nil {
		return // silently drop; logging shouldn't crash the agent
	}

	// Write as Markdown-friendly JSON block for easy human + machine reading.
	line := fmt.Sprintf("```json\n%s\n```\n\n", mustJSON(e))
	f.WriteString(line)
}

// LogBug records an error or failure.
func (l *Logger) LogBug(event, data string, meta Meta) {
	l.Log(Entry{Category: "bug", Event: event, Data: data, Meta: meta})
}

// LogInfo records a decision, milestone, or finding.
func (l *Logger) LogInfo(event, data string, meta Meta) {
	l.Log(Entry{Category: "info", Event: event, Data: data, Meta: meta})
}

// LogRuntime records execution state transitions.
func (l *Logger) LogRuntime(event, data string, meta Meta) {
	l.Log(Entry{Category: "runtime", Event: event, Data: data, Meta: meta})
}

// LogToolCall records a tool invocation and its result.
func (l *Logger) LogToolCall(event, data string, meta Meta) {
	l.Log(Entry{Category: "toolcall", Event: event, Data: data, Meta: meta})
}

// LogThink records a reasoning trace.
func (l *Logger) LogThink(event, data string, meta Meta) {
	l.Log(Entry{Category: "thinks", Event: event, Data: data, Meta: meta})
}

// LogUser records user interactions.
func (l *Logger) LogUser(event, data string, meta Meta) {
	l.Log(Entry{Category: "user", Event: event, Data: data, Meta: meta})
}

// LogTodo records task tracking events.
func (l *Logger) LogTodo(event, data string, meta Meta) {
	l.Log(Entry{Category: "todo", Event: event, Data: data, Meta: meta})
}

// LogChange records file modifications.
func (l *Logger) LogChange(event, data string, meta Meta) {
	l.Log(Entry{Category: "changes", Event: event, Data: data, Meta: meta})
}

// Close flushes and closes all log files.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	var lastErr error
	for cat, f := range l.files {
		if err := f.Close(); err != nil {
			lastErr = err
		}
		delete(l.files, cat)
	}
	return lastErr
}

// Dir returns the session log directory path.
func (l *Logger) Dir() string { return l.dir }

// SessionID returns the session ID.
func (l *Logger) SessionID() string { return l.sessionID }

// ── helpers ─────────────────────────────────────────────────────────────────

func (l *Logger) getFile(cat string) (*os.File, error) {
	if f, ok := l.files[cat]; ok {
		return f, nil
	}
	path := filepath.Join(l.dir, cat+".md")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	// Write header on first open.
	f.WriteString(fmt.Sprintf("# %s\n\n", catName(cat)))
	l.files[cat] = f
	return f, nil
}

func catName(cat string) string {
	names := map[string]string{
		"bug":      "Bug & Error Log",
		"info":     "Info & Decisions",
		"runtime":  "Runtime State Transitions",
		"toolcall": "Tool Call Trace",
		"thinks":   "Reasoning Traces",
		"user":     "User Interactions",
		"todo":     "Task Tracking",
		"changes":  "File & Git Changes",
		"summary":  "Session Summary",
	}
	if n, ok := names[cat]; ok {
		return n
	}
	return cat
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf(`{"error":"marshal failed: %v"}`, err)
	}
	return string(b)
}
