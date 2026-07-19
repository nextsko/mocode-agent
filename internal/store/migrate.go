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
	defer sqlDB.Close()

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
	st.Sessions().saveIndex()

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
		os.MkdirAll(sessDir, 0o700)
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
		os.MkdirAll(sessDir, 0o700)
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
	sessionRoot := filepath.Join(infra.Dir(), ".mocode", "sessions")
	if entries, err := os.ReadDir(sessionRoot); err == nil {
		fmt.Fprintf(os.Stderr, "Migrating per-session databases from %s...\n", sessionRoot)
		sessionMsgs := 0
		sessionFiles := 0
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			dbFile := filepath.Join(sessionRoot, entry.Name(), "mocode.db")
			if _, err := os.Stat(dbFile); os.IsNotExist(err) {
				continue
			}

			// Extract session ID from directory name: <uuid>_<date>
			parts := strings.SplitN(entry.Name(), "_", 2)
			if len(parts) < 1 {
				continue
			}
			sid := parts[0]

			// Find matching session in our index
			if _, ok := st.Sessions().index.Sessions[sid]; !ok {
				continue
			}

			perDB, err := sql.Open("sqlite", dbFile)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  Warning: cannot open %s: %v\n", dbFile, err)
				continue
			}

			// Read messages from per-session DB
			msgs, err := readMessages(ctx, perDB, sid)
			perDB.Close()
			if err != nil {
				fmt.Fprintf(os.Stderr, "  Warning: failed to read messages from %s: %v\n", dbFile, err)
				continue
			}
			if len(msgs) == 0 {
				continue
			}

			sessMeta := st.Sessions().index.Sessions[sid]
			sessDir := st.sessionDir(sessionMetaToSession(*sessMeta))
			os.MkdirAll(sessDir, 0o700)

			writer := NewJSONLWriter(filepath.Join(sessDir, "messages.jsonl"))
			for _, jm := range msgs {
				if err := writer.Append(jm); err != nil {
					fmt.Fprintf(os.Stderr, "  Warning: write message: %v\n", err)
				} else {
					sessionMsgs++
				}
			}
			sessionFiles += len(msgs) // count message count for stats
		}
		fmt.Fprintf(os.Stderr, "  Migrated %d messages from per-session databases\n", sessionMsgs)
	}

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
	return jsonlFile{
		ID:        f.ID,
		SessionID: f.SessionID,
		Path:      f.Path,
		Content:   f.Content,
		Version:   f.Version,
		CreatedAt: f.CreatedAt,
		UpdatedAt: f.UpdatedAt,
	}
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
		return nil, err
	}
	defer rows.Close()

	var result []migrationSession
	for rows.Next() {
		var s migrationSession
		if err := rows.Scan(&s.ID, &s.ParentSessionID, &s.Title, &s.MessageCount,
			&s.PromptTokens, &s.CompletionTokens, &s.SummaryMessageID, &s.Cost,
			&s.Todos, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, rows.Err()
}

func readMessages(ctx context.Context, db *sql.DB, sessionID string) ([]jsonlMessage, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, session_id, role, parts,
		        COALESCE(model,''), COALESCE(provider,''),
		        created_at, updated_at,
		        COALESCE(finished_at,0), COALESCE(is_summary_message,0)
		 FROM messages WHERE session_id = ? ORDER BY created_at`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []jsonlMessage
	for rows.Next() {
		var m migrationMessage
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Parts,
			&m.Model, &m.Provider, &m.CreatedAt, &m.UpdatedAt,
			&m.FinishedAt, &m.IsSummaryMessage); err != nil {
			return nil, err
		}
		result = append(result, m.toJSONL())
	}
	return result, rows.Err()
}

func readFiles(ctx context.Context, db *sql.DB, sessionID string) ([]jsonlFile, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, session_id, path, content, version, created_at, updated_at
		 FROM files WHERE session_id = ? ORDER BY created_at`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []jsonlFile
	for rows.Next() {
		var f migrationFile
		if err := rows.Scan(&f.ID, &f.SessionID, &f.Path, &f.Content,
			&f.Version, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, f.toJSONL())
	}
	return result, rows.Err()
}

// migrate_message ensures message import is used
var _ = message.Message{}
