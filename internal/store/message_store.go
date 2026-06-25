package store

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"github.com/package-register/mocode/internal/domain/session/message"
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
		Model:            string(params.Model),
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
			if err := json.Unmarshal([]byte(line), &jm); err != nil {
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
		if err := json.Unmarshal([]byte(line), &jm); err != nil {
			return nil // skip corrupt
		}
		msg, err := jsonlToMessage(jm)
		if err != nil {
			return nil
		}
		msgs = append(msgs, msg)
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
		return nil, nil
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

	return writer.WriteAll(newValues)
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
	return writer.WriteAll(newValues)
}

// DeleteSessionMessages removes all messages for a session.
func (ms *MessageStore) DeleteSessionMessages(ctx context.Context, sessionID string) error {
	writer := NewJSONLWriter(ms.messagesPath(sessionID))
	return writer.Remove()
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
	return writer.WriteAll(kept)
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
	return writer.WriteAll(kept)
}

// jsonlToMessage converts a jsonlMessage to a domain message.Message.
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
