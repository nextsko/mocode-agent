package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/nextsko/mocode-agent/internal/domain/session"
	"github.com/nextsko/mocode-agent/internal/domain/session/message"
	"github.com/nextsko/mocode-agent/internal/util/infra"
)

// MigrateFromSQLite exports all data from an old SQLite database into the
// new file-based store layout. It reads the SQLite DB, creates the store
// directory tree, and writes JSONL + index files.
func MigrateFromSQLite(ctx context.Context, dbPath string, projectPath string) (*Store, error) {
	fmt.Fprintf(os.Stderr, "Opening SQLite database: %s\n", dbPath)
	sqlDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	defer func() { _ = sqlDB.Close() }()

	// store.New resolves the data dir via infra.DataDir itself; no config needed.
	st, err := New(projectPath, nil)
	if err != nil {
		return nil, fmt.Errorf("create store: %w", err)
	}

	// Phase 1: Migrate sessions
	fmt.Fprintf(os.Stderr, "Migrating sessions...\n")
	sessions, err := readSessions(ctx, sqlDB)
	if err != nil {
		return nil, fmt.Errorf("read sessions: %w", err)
	}
	fmt.Fprintf(os.Stderr, "  Found %d sessions\n", len(sessions))

	for _, sm := range sessions {
		if err := st.Sessions().writeMeta(sm.toSession()); err != nil {
			return nil, fmt.Errorf("write session %s: %w", sm.ID, err)
		}
		st.Sessions().index.Sessions[sm.ID] = sm.toMeta()
	}
	_ = st.Sessions().saveIndex()

	// Phase 2: Migrate messages
	fmt.Fprintf(os.Stderr, "Migrating messages...\n")
	totalMsgs := 0
	for _, sm := range sessions {
		msgs, err := readMessages(ctx, sqlDB, sm.ID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: failed to read messages for session %s: %v\n", sm.ID, err)
			continue
		}
		if len(msgs) == 0 {
			continue
		}

		sessDir := st.sessionDir(sm.toSession())
		_ = os.MkdirAll(sessDir, 0o700)
		writer := NewJSONLWriter(filepath.Join(sessDir, "messages.jsonl"))
		for _, jm := range msgs {
			if err := writer.Append(jm); err != nil {
				return nil, fmt.Errorf("write message %s: %w", jm.ID, err)
			}
			totalMsgs++
		}
	}
	fmt.Fprintf(os.Stderr, "  Migrated %d messages\n", totalMsgs)

	// Phase 3: Migrate files (history)
	fmt.Fprintf(os.Stderr, "Migrating file history...\n")
	totalFiles := 0
	for _, sm := range sessions {
		files, err := readFiles(ctx, sqlDB, sm.ID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: failed to read files for session %s: %v\n", sm.ID, err)
			continue
		}
		if len(files) == 0 {
			continue
		}

		sessDir := st.sessionDir(sm.toSession())
		_ = os.MkdirAll(sessDir, 0o700)
		writer := NewJSONLWriter(filepath.Join(sessDir, "files.jsonl"))
		for _, jf := range files {
			if err := writer.Append(jf); err != nil {
				return nil, fmt.Errorf("write file %s: %w", jf.ID, err)
			}
			totalFiles++
		}
	}
	fmt.Fprintf(os.Stderr, "  Migrated %d file versions\n", totalFiles)

	// Phase 4: Migrate per-session databases (messages + files from old session stores)
	// Legacy migration path: reads from the pre-DataDirectory era, where
	// sessions lived under "<homedir>/.mocode/sessions/<id>/mocode.db".
	// Keep this literal until the next major migration removes it.
	migrateSessionDatabases(ctx, st)

	// Backup the old database
	backupPath := dbPath + ".bak"
	fmt.Fprintf(os.Stderr, "Backing up old database to: %s\n", backupPath)
	if err := os.Rename(dbPath, backupPath); err != nil {
		fmt.Fprintf(os.Stderr, "  Warning: could not rename old database: %v\n", err)
	} else {
		fmt.Fprintf(os.Stderr, "  Old database backed up. Delete %s when ready.\n", backupPath)
	}

	fmt.Fprintf(os.Stderr, "\nMigration complete!\n")
	fmt.Fprintf(os.Stderr, "  Sessions: %d\n", len(sessions))
	fmt.Fprintf(os.Stderr, "  Messages: %d\n", totalMsgs)
	fmt.Fprintf(os.Stderr, "  File versions: %d\n", totalFiles)
	fmt.Fprintf(os.Stderr, "  Data directory: %s\n", st.ProjectDir)

	return st, nil
}

// migrateSessionDatabases imports messages from the legacy per-session SQLite
// databases stored under "<homedir>/.mocode/sessions/<id>/mocode.db".
// A missing directory simply means there is nothing to migrate.
func migrateSessionDatabases(ctx context.Context, st *Store) {
	sessionRoot := filepath.Join(infra.Dir(), ".mocode", "sessions")
	entries, err := os.ReadDir(sessionRoot)
	if err != nil {
		return
	}

	fmt.Fprintf(os.Stderr, "Migrating per-session databases from %s...\n", sessionRoot)
	sessionMsgs := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dbFile := filepath.Join(sessionRoot, entry.Name(), "mocode.db")
		if _, statErr := os.Stat(dbFile); os.IsNotExist(statErr) {
			continue
		}

		// Extract session ID from directory name: <uuid>_<date>.
		parts := strings.SplitN(entry.Name(), "_", 2)
		if len(parts) < 1 {
			continue
		}
		sid := parts[0]

		// Only import databases that match a session in the new index.
		meta, ok := st.Sessions().index.Sessions[sid]
		if !ok {
			continue
		}

		sessionMsgs += importLegacySessionDB(ctx, st, dbFile, sid, *meta)
	}
	fmt.Fprintf(os.Stderr, "  Migrated %d messages from per-session databases\n", sessionMsgs)
}

// importLegacySessionDB copies the messages of one legacy per-session database
// into the JSONL layout and returns the number of messages written.
func importLegacySessionDB(ctx context.Context, st *Store, dbFile, sid string, meta sessionMeta) int {
	perDB, err := sql.Open("sqlite", dbFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  Warning: cannot open %s: %v\n", dbFile, err)
		return 0
	}
	defer func() { _ = perDB.Close() }()

	msgs, err := readMessages(ctx, perDB, sid)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  Warning: failed to read messages from %s: %v\n", dbFile, err)
		return 0
	}
	if len(msgs) == 0 {
		return 0
	}

	sessDir := st.sessionDir(sessionMetaToSession(meta))
	_ = os.MkdirAll(sessDir, 0o700)

	written := 0
	writer := NewJSONLWriter(filepath.Join(sessDir, "messages.jsonl"))
	for _, jm := range msgs {
		if err := writer.Append(jm); err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: write message: %v\n", err)
			continue
		}
		written++
	}
	return written
}

// --- SQLite row types (minimal — mirrors old db.Models) ---

type migrationSession struct {
	ID               string
	ParentSessionID  string
	Title            string
	MessageCount     int64
	PromptTokens     int64
	CompletionTokens int64
	SummaryMessageID string
	Cost             float64
	Todos            string // JSON
	CreatedAt        int64
	UpdatedAt        int64
}

func (s migrationSession) toSession() session.Session {
	return session.Session{
		ID:               s.ID,
		ParentSessionID:  s.ParentSessionID,
		Title:            s.Title,
		MessageCount:     s.MessageCount,
		PromptTokens:     s.PromptTokens,
		CompletionTokens: s.CompletionTokens,
		SummaryMessageID: s.SummaryMessageID,
		Cost:             s.Cost,
		CreatedAt:        s.CreatedAt,
		UpdatedAt:        s.UpdatedAt,
	}
}

func (s migrationSession) toMeta() *sessionMeta {
	return &sessionMeta{
		ID:               s.ID,
		ParentSessionID:  s.ParentSessionID,
		Title:            s.Title,
		MessageCount:     s.MessageCount,
		PromptTokens:     s.PromptTokens,
		CompletionTokens: s.CompletionTokens,
		SummaryMessageID: s.SummaryMessageID,
		Cost:             s.Cost,
		CreatedAt:        s.CreatedAt,
		UpdatedAt:        s.UpdatedAt,
	}
}

type migrationMessage struct {
	ID               string
	SessionID        string
	Role             string
	Parts            string // JSON
	Model            string
	Provider         string
	CreatedAt        int64
	UpdatedAt        int64
	FinishedAt       int64
	IsSummaryMessage int64
}

func (m migrationMessage) toJSONL() jsonlMessage {
	return jsonlMessage{
		ID:               m.ID,
		SessionID:        m.SessionID,
		Role:             m.Role,
		Parts:            json.RawMessage(m.Parts),
		Model:            m.Model,
		Provider:         m.Provider,
		CreatedAt:        m.CreatedAt,
		UpdatedAt:        m.UpdatedAt,
		FinishedAt:       m.FinishedAt,
		IsSummaryMessage: m.IsSummaryMessage != 0,
	}
}

type migrationFile struct {
	ID        string
	SessionID string
	Path      string
	Content   string
	Version   int64
	CreatedAt int64
	UpdatedAt int64
}

func (f migrationFile) toJSONL() jsonlFile {
	return jsonlFile(f)
}

// --- SQLite readers ---

func readSessions(ctx context.Context, db *sql.DB) ([]migrationSession, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, COALESCE(parent_session_id,''), title, message_count,
		        prompt_tokens, completion_tokens,
		        COALESCE(summary_message_id,''), cost,
		        COALESCE(todos,''), created_at, updated_at
		 FROM sessions ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("query sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var result []migrationSession
	for rows.Next() {
		var s migrationSession
		if err := rows.Scan(&s.ID, &s.ParentSessionID, &s.Title, &s.MessageCount,
			&s.PromptTokens, &s.CompletionTokens, &s.SummaryMessageID, &s.Cost,
			&s.Todos, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		result = append(result, s)
	}
	if err := rows.Err(); err != nil {
		return result, fmt.Errorf("iterate sessions: %w", err)
	}
	return result, nil
}

func readMessages(ctx context.Context, db *sql.DB, sessionID string) ([]jsonlMessage, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, session_id, role, parts,
		        COALESCE(model,''), COALESCE(provider,''),
		        created_at, updated_at,
		        COALESCE(finished_at,0), COALESCE(is_summary_message,0)
		 FROM messages WHERE session_id = ? ORDER BY created_at`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("query messages: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var result []jsonlMessage
	for rows.Next() {
		var m migrationMessage
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Parts,
			&m.Model, &m.Provider, &m.CreatedAt, &m.UpdatedAt,
			&m.FinishedAt, &m.IsSummaryMessage); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		result = append(result, m.toJSONL())
	}
	if err := rows.Err(); err != nil {
		return result, fmt.Errorf("iterate messages: %w", err)
	}
	return result, nil
}

func readFiles(ctx context.Context, db *sql.DB, sessionID string) ([]jsonlFile, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, session_id, path, content, version, created_at, updated_at
		 FROM files WHERE session_id = ? ORDER BY created_at`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("query files: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var result []jsonlFile
	for rows.Next() {
		var f migrationFile
		if err := rows.Scan(&f.ID, &f.SessionID, &f.Path, &f.Content,
			&f.Version, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan file: %w", err)
		}
		result = append(result, f.toJSONL())
	}
	if err := rows.Err(); err != nil {
		return result, fmt.Errorf("iterate files: %w", err)
	}
	return result, nil
}

// migrate_message ensures message import is used.
var _ = message.Message{}
