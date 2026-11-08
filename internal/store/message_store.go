package store

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"github.com/nextsko/mocode-agent/internal/domain/session/message"
)

// MessageStore provides file-based message persistence via JSONL.
type MessageStore struct {
	store *Store
}

func newMessageStore(s *Store) *MessageStore {
	return &MessageStore{store: s}
}

// jsonlMessage is the on-disk representation of a message.
type jsonlMessage struct {
	ID               string          `json:"id"`
	SessionID        string          `json:"session_id"`
	Role             string          `json:"role"`
	Parts            json.RawMessage `json:"parts"`
	Model            string          `json:"model,omitempty"`
	Provider         string          `json:"provider,omitempty"`
	Sender           string          `json:"sender,omitempty"`
	CreatedAt        int64           `json:"created_at"`
	UpdatedAt        int64           `json:"updated_at"`
	FinishedAt       int64           `json:"finished_at,omitempty"`
	IsSummaryMessage bool            `json:"is_summary_message,omitempty"`
}

func (ms *MessageStore) messagesPath(sessionID string) string {
	// Look up the actual directory name from the session index
	if meta, ok := ms.store.Sessions().index.Sessions[sessionID]; ok {
		dir := ms.store.sessionDir(sessionMetaToSession(*meta))
		return filepath.Join(dir, "messages.jsonl")
	}
	// Fallback: try the sessionID directly (for migration/new sessions)
	return filepath.Join(ms.store.ProjectDir, "sessions", sessionID, "messages.jsonl")
}

func (ms *MessageStore) sessionDirPath(sessionID string) string {
	if meta, ok := ms.store.Sessions().index.Sessions[sessionID]; ok {
		return ms.store.sessionDir(sessionMetaToSession(*meta))
	}
	return filepath.Join(ms.store.ProjectDir, "sessions", sessionID)
}

// Create appends a new message to the session's JSONL file.
func (ms *MessageStore) Create(ctx context.Context, sessionID string, params message.CreateMessageParams) (message.Message, error) {
	now := time.Now().Unix()

	// Marshal parts to JSON
	partsJSON, err := marshalParts(params.Parts)
	if err != nil {
		return message.Message{}, err
	}

	msg := message.Message{
		ID:               uuid.New().String(),
		SessionID:        sessionID,
		Role:             params.Role,
		Parts:            params.Parts,
		Model:            params.Model,
		Provider:         params.Provider,
		Sender:           params.Sender,
		CreatedAt:        now,
		UpdatedAt:        now,
		IsSummaryMessage: params.IsSummaryMessage,
	}

	jmsg := jsonlMessage{
		ID:               msg.ID,
		SessionID:        msg.SessionID,
		Role:             string(msg.Role),
		Parts:            partsJSON,
		Model:            msg.Model,
		Provider:         msg.Provider,
		Sender:           msg.Sender,
		CreatedAt:        msg.CreatedAt,
		UpdatedAt:        msg.UpdatedAt,
		IsSummaryMessage: msg.IsSummaryMessage,
	}

	writer := NewJSONLWriter(ms.messagesPath(sessionID))
	if err := writer.Append(jmsg); err != nil {
		return message.Message{}, fmt.Errorf("append message: %w", err)
	}
	ms.indexMessage(ctx, jmsg)

	return msg, nil
}

// Get retrieves a message by ID (scans the session's JSONL).
func (ms *MessageStore) Get(ctx context.Context, id string) (message.Message, error) {
	// Scan all sessions — could be optimized with a message index
	dir := filepath.Join(ms.store.ProjectDir, "sessions")
	entries, err := readDir(dir)
	if err != nil {
		return message.Message{}, fmt.Errorf("message not found: %s", id)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		mpath := filepath.Join(dir, entry.Name(), "messages.jsonl")
		writer := NewJSONLWriter(mpath)
		if !writer.Exists() {
			continue
		}
		var found *jsonlMessage
		_ = writer.ScanLines(func(line string) error {
			var jm jsonlMessage
			if !decodeJSONLine(line, &jm) {
				return nil // skip corrupt lines
			}
			if jm.ID == id {
				found = &jm
				return fmt.Errorf("found") // early exit
			}
			return nil
		})
		if found != nil {
			return jsonlToMessage(*found)
		}
	}
	return message.Message{}, fmt.Errorf("message not found: %s", id)
}

// List returns all messages for a session, ordered by created_at.
func (ms *MessageStore) List(ctx context.Context, sessionID string) ([]message.Message, error) {
	var msgs []message.Message
	writer := NewJSONLWriter(ms.messagesPath(sessionID))
	if !writer.Exists() {
		return msgs, nil
	}
	err := writer.ScanLines(func(line string) error {
		var jm jsonlMessage
		if !decodeJSONLine(line, &jm) {
			return nil // skip corrupt
		}
		if msg, err := jsonlToMessage(jm); err == nil {
			msgs = append(msgs, msg)
		}
		return nil
	})
	return msgs, err
}

// ListUserMessages returns only user messages for a session.
func (ms *MessageStore) ListUserMessages(ctx context.Context, sessionID string) ([]message.Message, error) {
	all, err := ms.List(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	var userMsgs []message.Message
	for _, m := range all {
		if m.Role == message.User {
			userMsgs = append(userMsgs, m)
		}
	}
	return userMsgs, nil
}

// ListAllUserMessages returns user messages across all sessions.
func (ms *MessageStore) ListAllUserMessages(ctx context.Context) ([]message.Message, error) {
	dir := filepath.Join(ms.store.ProjectDir, "sessions")
	entries, err := readDir(dir)
	if err != nil {
		// A missing or unreadable sessions directory simply has no messages.
		return nil, nil //nolint:nilerr // Soft-fail by design: absence of sessions is an empty list.
	}
	var all []message.Message
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		msgs, _ := ms.ListUserMessages(ctx, entry.Name())
		all = append(all, msgs...)
	}
	return all, nil
}

// Update rewrites a message's parts and finished_at in the JSONL file.
func (ms *MessageStore) Update(ctx context.Context, msg message.Message) error {
	// Read all messages, find and replace the target
	writer := NewJSONLWriter(ms.messagesPath(msg.SessionID))
	all, err := writer.ReadAll(func() any { return &jsonlMessage{} })
	if err != nil {
		return err
	}

	partsJSON, err := marshalParts(msg.Parts)
	if err != nil {
		return err
	}

	var newValues []any
	found := false
	for _, v := range all {
		jm, ok := v.(*jsonlMessage)
		if !ok {
			continue
		}
		if jm.ID == msg.ID {
			jm.Parts = partsJSON
			jm.UpdatedAt = time.Now().Unix()
			// Set finished_at from finish part if present
			if fp := msg.FinishPart(); fp != nil {
				jm.FinishedAt = fp.Time
			}
			found = true
		}
		newValues = append(newValues, jm)
	}
	if !found {
		return fmt.Errorf("message not found: %s", msg.ID)
	}
	if err := writer.WriteAll(newValues); err != nil {
		return err
	}
	if jm := findJSONLMessage(newValues, msg.ID); jm != nil {
		ms.indexMessage(ctx, *jm)
	}
	return nil
}

// Delete removes a message from the JSONL file.
func (ms *MessageStore) Delete(ctx context.Context, id string) error {
	msg, err := ms.Get(ctx, id)
	if err != nil {
		return err
	}
	writer := NewJSONLWriter(ms.messagesPath(msg.SessionID))
	all, err := writer.ReadAll(func() any { return &jsonlMessage{} })
	if err != nil {
		return err
	}
	var newValues []any
	for _, v := range all {
		jm, ok := v.(*jsonlMessage)
		if !ok {
			continue
		}
		if jm.ID != id {
			newValues = append(newValues, jm)
		}
	}
	if err := writer.WriteAll(newValues); err != nil {
		return err
	}
	ms.deleteIndexedMessage(ctx, id)
	return nil
}

// DeleteSessionMessages removes all messages for a session.
func (ms *MessageStore) DeleteSessionMessages(ctx context.Context, sessionID string) error {
	writer := NewJSONLWriter(ms.messagesPath(sessionID))
	if err := writer.Remove(); err != nil {
		return err
	}
	ms.deleteIndexedSession(ctx, sessionID)
	return nil
}

// DeleteAfter removes all messages after the given index (0-based, exclusive).
// Keeps messages at indices [0, afterIndex].
func (ms *MessageStore) DeleteAfter(ctx context.Context, sessionID string, afterIndex int) error {
	if afterIndex < 0 {
		return fmt.Errorf("invalid index: %d", afterIndex)
	}

	writer := NewJSONLWriter(ms.messagesPath(sessionID))
	all, err := writer.ReadAll(func() any { return &jsonlMessage{} })
	if err != nil {
		return err
	}

	var kept []any
	for i, v := range all {
		if i <= afterIndex {
			kept = append(kept, v)
		}
	}
	if err := writer.WriteAll(kept); err != nil {
		return err
	}
	ms.reindexSession(ctx, sessionID, toJSONLMessages(kept))
	return nil
}

// DeleteAfterMessage removes all messages after the message with the given ID.
// Keeps the target message and all messages before it.
func (ms *MessageStore) DeleteAfterMessage(ctx context.Context, sessionID string, messageID string) error {
	writer := NewJSONLWriter(ms.messagesPath(sessionID))
	all, err := writer.ReadAll(func() any { return &jsonlMessage{} })
	if err != nil {
		return err
	}

	var kept []any
	found := false
	for _, v := range all {
		jm, ok := v.(*jsonlMessage)
		if !ok {
			continue
		}
		kept = append(kept, jm)
		if jm.ID == messageID {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("message not found: %s", messageID)
	}
	if err := writer.WriteAll(kept); err != nil {
		return err
	}
	ms.reindexSession(ctx, sessionID, toJSONLMessages(kept))
	return nil
}

// jsonlToMessage converts a jsonlMessage to a domain message.Message.
// indexMessage keeps the derived FTS index in step with the JSONL truth. The
// index is a cache, so a failure is logged instead of failing the write.
func (ms *MessageStore) indexMessage(ctx context.Context, jm jsonlMessage) {
	se := ms.store.Search()
	if se == nil {
		return
	}
	if err := se.IndexMessage(ctx, jm); err != nil {
		slog.Warn("Failed to update session search index", "message", jm.ID, "error", err)
	}
}

// deleteIndexedMessage removes one message from the derived FTS index.
func (ms *MessageStore) deleteIndexedMessage(ctx context.Context, id string) {
	se := ms.store.Search()
	if se == nil {
		return
	}
	if err := se.DeleteMessage(ctx, id); err != nil {
		slog.Warn("Failed to remove message from session search index", "message", id, "error", err)
	}
}

// deleteIndexedSession removes one session from the derived FTS index.
func (ms *MessageStore) deleteIndexedSession(ctx context.Context, sessionID string) {
	se := ms.store.Search()
	if se == nil {
		return
	}
	if err := se.DeleteSession(ctx, sessionID); err != nil {
		slog.Warn("Failed to remove session from search index", "session", sessionID, "error", err)
	}
}

// reindexSession replaces one session's rows in the derived FTS index after a
// rewrite of its JSONL file.
func (ms *MessageStore) reindexSession(ctx context.Context, sessionID string, msgs []jsonlMessage) {
	se := ms.store.Search()
	if se == nil {
		return
	}
	if err := se.ReindexSession(ctx, sessionID, msgs); err != nil {
		slog.Warn("Failed to reindex session search", "session", sessionID, "error", err)
	}
}

// findJSONLMessage returns the message with id, or nil.
func findJSONLMessage(values []any, id string) *jsonlMessage {
	for _, v := range values {
		if jm, ok := v.(*jsonlMessage); ok && jm.ID == id {
			return jm
		}
	}
	return nil
}

// toJSONLMessages converts rewritten values back to typed messages.
func toJSONLMessages(values []any) []jsonlMessage {
	msgs := make([]jsonlMessage, 0, len(values))
	for _, v := range values {
		if jm, ok := v.(*jsonlMessage); ok {
			msgs = append(msgs, *jm)
		}
	}
	return msgs
}

func jsonlToMessage(jm jsonlMessage) (message.Message, error) {
	parts, err := unmarshalParts(jm.Parts)
	if err != nil {
		return message.Message{}, err
	}
	return message.Message{
		ID:               jm.ID,
		SessionID:        jm.SessionID,
		Role:             message.MessageRole(jm.Role),
		Parts:            parts,
		Model:            jm.Model,
		Provider:         jm.Provider,
		Sender:           jm.Sender,
		CreatedAt:        jm.CreatedAt,
		UpdatedAt:        jm.UpdatedAt,
		IsSummaryMessage: jm.IsSummaryMessage,
	}, nil
}
