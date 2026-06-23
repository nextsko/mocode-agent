package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"encoding/json"

	_ "modernc.org/sqlite"
)

// SessionSearch provides full-text search over session messages using SQLite FTS5.
// It builds a lightweight auxiliary index from the existing JSONL message files,
// keeping the primary storage (JSONL) untouched.
type SessionSearch struct {
	store *Store
	db    *sql.DB
	mu    sync.RWMutex
}

// SearchResult represents a single FTS5 match with context.
type SearchResult struct {
	SessionID    string  `json:"session_id"`
	SessionTitle string  `json:"session_title"`
	MessageID    string  `json:"message_id"`
	Role         string  `json:"role"`
	Content      string  `json:"content"`
	Snippet      string  `json:"snippet"`
	Timestamp    int64   `json:"timestamp"`
	Rank         float64 `json:"rank"`
}

// SearchResponse is returned by the session_search tool.
type SearchResponse struct {
	Mode     string         `json:"mode"`
	Query    string         `json:"query,omitempty"`
	Results  []SearchResult `json:"results,omitempty"`
	Sessions []SessionBrief `json:"sessions,omitempty"`
	Total    int            `json:"total"`
}

// SessionBrief is a summary for browse mode.
type SessionBrief struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	UpdatedAt    int64  `json:"updated_at"`
	MessageCount int64  `json:"message_count"`
}

func newSessionSearch(s *Store) *SessionSearch {
	return &SessionSearch{store: s}
}

func (ss *SessionSearch) dbPath() string {
	return filepath.Join(ss.store.ProjectDir, "session_search.db")
}

// Open initializes the SQLite database and creates FTS5 tables if needed.
func (ss *SessionSearch) Open(ctx context.Context) error {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	if ss.db != nil {
		return nil
	}

	dbPath := ss.dbPath()
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return fmt.Errorf("open session search db: %w", err)
	}

	schema := `
		CREATE TABLE IF NOT EXISTS messages (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL DEFAULT '',
			tool_name TEXT DEFAULT '',
			created_at INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id);
		CREATE INDEX IF NOT EXISTS idx_messages_created ON messages(created_at);
		CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
			content,
			tool_name,
			content='messages',
			content_rowid='rowid',
			tokenize='unicode61'
		);
		CREATE TRIGGER IF NOT EXISTS messages_ai AFTER INSERT ON messages BEGIN
			INSERT INTO messages_fts(rowid, content, tool_name) VALUES (new.rowid, new.content, new.tool_name);
		END;
		CREATE TRIGGER IF NOT EXISTS messages_ad AFTER DELETE ON messages BEGIN
			INSERT INTO messages_fts(messages_fts, rowid, content, tool_name) VALUES('delete', old.rowid, old.content, old.tool_name);
		END;
		CREATE TRIGGER IF NOT EXISTS messages_au AFTER UPDATE ON messages BEGIN
			INSERT INTO messages_fts(messages_fts, rowid, content, tool_name) VALUES('delete', old.rowid, old.content, old.tool_name);
			INSERT INTO messages_fts(rowid, content, tool_name) VALUES (new.rowid, new.content, new.tool_name);
		END;
	`
	_, err = db.ExecContext(ctx, schema)
	if err != nil {
		db.Close()
		return fmt.Errorf("create session search schema: %w", err)
	}

	ss.db = db
	return nil
}

// Close closes the database connection.
func (ss *SessionSearch) Close() error {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if ss.db != nil {
		err := ss.db.Close()
		ss.db = nil
		return err
	}
	return nil
}

// DB returns the underlying database for testing.
func (ss *SessionSearch) DB() *sql.DB {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	return ss.db
}

// Rebuild scans all JSONL message files and rebuilds the FTS5 index.
func (ss *SessionSearch) Rebuild(ctx context.Context) error {
	if err := ss.Open(ctx); err != nil {
		return err
	}

	ss.mu.Lock()
	defer ss.mu.Unlock()

	tx, err := ss.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin rebuild tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, "DELETE FROM messages"); err != nil {
		return fmt.Errorf("clear messages: %w", err)
	}

	sessionsDir := ss.store.sessionsDir()
	entries, err := readDir(sessionsDir)
	if err != nil {
		return nil
	}

	stmt, err := tx.PrepareContext(ctx, `INSERT INTO messages (id, session_id, role, content, tool_name, created_at) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare insert: %w", err)
	}
	defer stmt.Close()

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		mpath := filepath.Join(sessionsDir, entry.Name(), "messages.jsonl")
		writer := NewJSONLWriter(mpath)
		if !writer.Exists() {
			continue
		}
		err := writer.ScanLines(func(line string) error {
			var jm jsonlMessage
			if err := json.Unmarshal([]byte(line), &jm); err != nil {
				return nil
			}
			content := extractSearchText(jm.Parts)
			toolName := extractToolName(jm.Parts)
			if _, err := stmt.ExecContext(ctx, jm.ID, jm.SessionID, jm.Role, content, toolName, jm.CreatedAt); err != nil {
				return fmt.Errorf("insert message %s: %w", jm.ID, err)
			}
			return nil
		})
		if err != nil {
			continue
		}
	}

	return tx.Commit()
}

// IndexMessage adds a single message to the FTS5 index.
func (ss *SessionSearch) IndexMessage(ctx context.Context, jm jsonlMessage) error {
	if err := ss.Open(ctx); err != nil {
		return err
	}
	ss.mu.Lock()
	defer ss.mu.Unlock()

	content := extractSearchText(jm.Parts)
	toolName := extractToolName(jm.Parts)
	_, err := ss.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO messages (id, session_id, role, content, tool_name, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		jm.ID, jm.SessionID, jm.Role, content, toolName, jm.CreatedAt)
	return err
}

// Search performs FTS5 full-text search across all sessions.
func (ss *SessionSearch) Search(ctx context.Context, query string, limit int) (*SearchResponse, error) {
	if err := ss.Open(ctx); err != nil {
		return nil, err
	}

	if ss.indexEmpty(ctx) {
		if err := ss.Rebuild(ctx); err != nil {
			return nil, fmt.Errorf("auto-rebuild index: %w", err)
		}
	}

	if limit <= 0 {
		limit = 10
	}

	ftsQuery := sanitizeFTSQuery(query)

	// Get the list of matching session IDs first for dedup
	matchRows, err := ss.db.QueryContext(ctx,
		`SELECT DISTINCT session_id FROM messages_fts WHERE messages_fts MATCH ? ORDER BY bm25(messages_fts) LIMIT ?`,
		ftsQuery, limit)
	if err != nil {
		return nil, fmt.Errorf("fts search sessions: %w", err)
	}
	defer matchRows.Close()

	var sessionIDs []string
	for matchRows.Next() {
		var sid string
		if err := matchRows.Scan(&sid); err != nil {
			continue
		}
		sessionIDs = append(sessionIDs, sid)
	}

	// For each session, get the best matching message
	results := make([]SearchResult, 0, len(sessionIDs))
	for _, sid := range sessionIDs {
		var r SearchResult
		var snippet sql.NullString
		err := ss.db.QueryRowContext(ctx,
			`SELECT m.session_id, m.id, m.role, m.content,
				snippet(messages_fts, 2, '<b>', '</b>', '...', 32),
				m.created_at, bm25(messages_fts)
			FROM messages_fts AS fts
			JOIN messages AS m ON m.rowid = fts.rowid
			WHERE messages_fts MATCH ? AND m.session_id = ?
			ORDER BY bm25(messages_fts) LIMIT 1`,
			ftsQuery, sid).Scan(&r.SessionID, &r.MessageID, &r.Role, &r.Content, &snippet, &r.Timestamp, &r.Rank)
		if err != nil {
			continue
		}
		if snippet.Valid {
			r.Snippet = snippet.String
		}
		// Get session title
		if meta, ok := ss.store.Sessions().index.Sessions[sid]; ok {
			r.SessionTitle = meta.Title
		}
		if len(r.Content) > 500 {
			r.Content = r.Content[:500] + "..."
		}
		results = append(results, r)
	}

	return &SearchResponse{
		Mode:    "discovery",
		Query:   query,
		Results: results,
		Total:   len(results),
	}, nil
}

// Scroll returns a window of messages around a given message in a session.
func (ss *SessionSearch) Scroll(ctx context.Context, sessionID, aroundMessageID string, window int) (*SearchResponse, error) {
	if err := ss.Open(ctx); err != nil {
		return nil, err
	}

	if window <= 0 {
		window = 5
	}

	var anchorRowid int
	err := ss.db.QueryRowContext(ctx,
		`SELECT rowid FROM messages WHERE id = ? AND session_id = ?`, aroundMessageID, sessionID).Scan(&anchorRowid)
	if err != nil {
		return nil, fmt.Errorf("anchor message not found: %w", err)
	}

	minRow := anchorRowid - window
	if minRow < 0 {
		minRow = 0
	}
	maxRow := anchorRowid + window

	rows, err := ss.db.QueryContext(ctx,
		`SELECT m.id, m.role, m.content, m.tool_name, m.created_at
		FROM messages m WHERE m.session_id = ? AND m.rowid BETWEEN ? AND ?
		ORDER BY m.rowid`, sessionID, minRow, maxRow)
	if err != nil {
		return nil, fmt.Errorf("scroll query: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		var toolName sql.NullString
		if err := rows.Scan(&r.MessageID, &r.Role, &r.Content, &toolName, &r.Timestamp); err != nil {
			continue
		}
		r.SessionID = sessionID
		prefix := ""
		if r.MessageID == aroundMessageID {
			prefix = "[anchor] "
		}
		r.Snippet = prefix + truncateContent(r.Content, 300)
		if len(r.Content) > 500 {
			r.Content = r.Content[:500] + "..."
		}
		results = append(results, r)
	}

	return &SearchResponse{
		Mode:    "scroll",
		Results: results,
		Total:   len(results),
	}, nil
}

// Browse returns recent sessions sorted by last activity.
func (ss *SessionSearch) Browse(ctx context.Context, limit int) (*SearchResponse, error) {
	if err := ss.Open(ctx); err != nil {
		return nil, err
	}

	if ss.indexEmpty(ctx) {
		if err := ss.Rebuild(ctx); err != nil {
			return nil, fmt.Errorf("auto-rebuild index: %w", err)
		}
	}

	if limit <= 0 {
		limit = 20
	}

	rows, err := ss.db.QueryContext(ctx,
		`SELECT m.session_id, COUNT(*) as msg_count, MAX(m.created_at) as last_activity
		FROM messages m GROUP BY m.session_id ORDER BY last_activity DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("browse query: %w", err)
	}
	defer rows.Close()

	var sessions []SessionBrief
	for rows.Next() {
		var s SessionBrief
		if err := rows.Scan(&s.ID, &s.MessageCount, &s.UpdatedAt); err != nil {
			continue
		}
		if meta, ok := ss.store.Sessions().index.Sessions[s.ID]; ok {
			s.Title = meta.Title
		}
		sessions = append(sessions, s)
	}

	return &SearchResponse{
		Mode:     "browse",
		Sessions: sessions,
		Total:    len(sessions),
	}, nil
}

func (ss *SessionSearch) indexEmpty(ctx context.Context) bool {
	var count int
	err := ss.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages`).Scan(&count)
	return err != nil || count == 0
}

func sanitizeFTSQuery(q string) string {
	q = strings.ReplaceAll(q, `"`, `""`)
	tokens := strings.Fields(q)
	if len(tokens) == 0 {
		return `""`
	}
	var escaped []string
	for _, t := range tokens {
		t = strings.TrimSpace(t)
		t = strings.Map(func(r rune) rune {
			if r == '(' || r == ')' || r == '*' || r == '^' || r == ':' || r == '{' || r == '}' {
				return ' '
			}
			return r
		}, t)
		if t != "" {
			escaped = append(escaped, `"`+t+`"`)
		}
	}
	return strings.Join(escaped, " AND ")
}

// extractSearchText gets all indexable text from message parts.
func extractSearchText(partsJSON json.RawMessage) string {
	var parts []struct {
		Type string `json:"type"`
		Data struct {
			Text     string `json:"text"`
			Thinking string `json:"thinking"`
			Input    string `json:"input"`
			Content  string `json:"content"`
			Name     string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(partsJSON, &parts); err != nil {
		return ""
	}

	var texts []string
	for _, p := range parts {
		switch p.Type {
		case "text":
			if p.Data.Text != "" {
				texts = append(texts, p.Data.Text)
			}
		case "reasoning":
			if p.Data.Thinking != "" {
				texts = append(texts, p.Data.Thinking)
			}
		case "tool_call":
			if p.Data.Name != "" {
				texts = append(texts, "tool:"+p.Data.Name)
			}
			if p.Data.Input != "" && len(p.Data.Input) < 1000 {
				texts = append(texts, p.Data.Input)
			}
		case "tool_result":
			if p.Data.Content != "" {
				c := p.Data.Content
				if len(c) > 500 {
					c = c[:500]
				}
				texts = append(texts, c)
			}
		}
	}
	return strings.Join(texts, "\n")
}

func extractToolName(partsJSON json.RawMessage) string {
	var parts []struct {
		Type string `json:"type"`
		Data struct {
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(partsJSON, &parts); err != nil {
		return ""
	}
	for _, p := range parts {
		if p.Type == "tool_call" && p.Data.Name != "" {
			return p.Data.Name
		}
	}
	return ""
}

func truncateContent(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

