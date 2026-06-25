package memory

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"charm.land/fantasy"
)

const (
	// DefaultMemoryLimit is the default limit of memories per user.
	DefaultMemoryLimit = 1000
	// DefaultMaxSearchResults is the default maximum number of search results.
	DefaultMaxSearchResults = 10
	// DefaultMinSearchScore is the default minimum search score.
	DefaultMinSearchScore = 0.3
)

// ServiceConfig holds configuration for the memory service.
type ServiceConfig struct {
	DB               *sql.DB
	MaxMemories      int
	MaxSearchResults int
	MinSearchScore   float64
	EnabledTools     map[string]bool
}

// service implements the Service interface with SQLite backend.
type service struct {
	db               *sql.DB
	maxMemories      int
	maxSearchResults int
	minSearchScore   float64
	enabledTools     map[string]bool
	cachedTools      map[string]fantasy.AgentTool
	toolsMu          sync.RWMutex
}

// NewService creates a new memory service with SQLite backend.
func NewService(cfg ServiceConfig) (Service, error) {
	if cfg.DB == nil {
		return nil, fmt.Errorf("database is required")
	}

	maxMemories := cfg.MaxMemories
	if maxMemories <= 0 {
		maxMemories = DefaultMemoryLimit
	}

	maxSearchResults := cfg.MaxSearchResults
	if maxSearchResults <= 0 {
		maxSearchResults = DefaultMaxSearchResults
	}

	minSearchScore := cfg.MinSearchScore
	if minSearchScore <= 0 {
		minSearchScore = DefaultMinSearchScore
	}

	enabledTools := cfg.EnabledTools
	if enabledTools == nil {
		enabledTools = make(map[string]bool)
		// Enable all tools by default except clear (dangerous)
		enabledTools[AddToolName] = true
		enabledTools[UpdateToolName] = true
		enabledTools[DeleteToolName] = true
		enabledTools[SearchToolName] = true
		enabledTools[LoadToolName] = true
	}

	s := &service{
		db:               cfg.DB,
		maxMemories:      maxMemories,
		maxSearchResults: maxSearchResults,
		minSearchScore:   minSearchScore,
		enabledTools:     enabledTools,
		cachedTools:      make(map[string]fantasy.AgentTool),
	}

	return s, nil
}

// generateMemoryID generates a unique ID for memory based on content and metadata.
func generateMemoryID(mem *Memory, appName, userID string) string {
	var builder strings.Builder
	builder.WriteString("memory:")
	builder.WriteString(mem.Memory)
	builder.WriteString("|app:")
	builder.WriteString(appName)
	builder.WriteString("|user:")
	builder.WriteString(userID)

	if kind := mem.Kind; kind != "" && kind != KindFact {
		builder.WriteString("|kind:")
		builder.WriteString(string(kind))
	}
	if mem.EventTime != nil {
		builder.WriteString("|event_time:")
		builder.WriteString(mem.EventTime.UTC().Format("2006-01-02T15:04:05Z07:00"))
	}
	if participants := normalizeParticipants(mem.Participants); len(participants) > 0 {
		builder.WriteString("|participants:")
		builder.WriteString(strings.Join(participants, ","))
	}
	if location := strings.TrimSpace(mem.Location); location != "" {
		builder.WriteString("|location:")
		builder.WriteString(location)
	}

	hash := sha256.Sum256([]byte(builder.String()))
	return fmt.Sprintf("%x", hash)
}

// normalizeParticipants normalizes and deduplicates participants.
func normalizeParticipants(participants []string) []string {
	if len(participants) == 0 {
		return nil
	}

	normalized := make([]string, 0, len(participants))
	seen := make(map[string]bool)

	for _, p := range participants {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		folded := strings.ToLower(p)
		if seen[folded] {
			continue
		}
		seen[folded] = true
		normalized = append(normalized, p)
	}

	sort.Strings(normalized)
	return normalized
}

// AddMemory adds or updates a memory for a user (idempotent).
func (s *service) AddMemory(ctx context.Context, appName, userID, memoryStr string,
	topics []string, kind Kind, eventTime *time.Time,
	participants []string, location string,
) error {
	if appName == "" {
		return ErrAppNameRequired
	}
	if userID == "" {
		return ErrUserIDRequired
	}
	if memoryStr == "" {
		return ErrMemoryRequired
	}

	now := time.Now()
	mem := &Memory{
		Memory:       memoryStr,
		Topics:       topics,
		LastUpdated:  &now,
		Kind:         kind,
		EventTime:    eventTime,
		Participants: normalizeParticipants(participants),
		Location:     strings.TrimSpace(location),
	}

	memoryID := generateMemoryID(mem, appName, userID)

	// Check memory limit
	if err := s.enforceMemoryLimit(ctx, appName, userID); err != nil {
		return err
	}

	entry := &Entry{
		ID:        memoryID,
		AppName:   appName,
		UserID:    userID,
		Memory:    mem,
		CreatedAt: now,
		UpdatedAt: now,
	}

	memoryData, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal memory entry: %w", err)
	}

	query := `
INSERT INTO memories (id, app_name, user_id, memory_data, created_at, updated_at, deleted_at)
VALUES (?, ?, ?, ?, ?, ?, NULL)
ON CONFLICT(id) DO UPDATE SET
  memory_data = excluded.memory_data,
  updated_at = excluded.updated_at,
  deleted_at = NULL`

	_, err = s.db.ExecContext(ctx, query,
		entry.ID, appName, userID, memoryData,
		now.UnixNano(), now.UnixNano())
	if err != nil {
		return fmt.Errorf("store memory entry: %w", err)
	}

	return nil
}

// UpdateMemory updates an existing memory for a user.
func (s *service) UpdateMemory(ctx context.Context, appName, userID, memoryID, memoryStr string,
	topics []string, kind Kind, eventTime *time.Time,
	participants []string, location string,
) error {
	if appName == "" {
		return ErrAppNameRequired
	}
	if userID == "" {
		return ErrUserIDRequired
	}
	if memoryID == "" {
		return ErrMemoryIDRequired
	}
	if memoryStr == "" {
		return ErrMemoryRequired
	}

	// Get existing entry
	entry, err := s.getEntry(ctx, appName, userID, memoryID)
	if err != nil {
		return err
	}

	now := time.Now()
	entry.Memory.Memory = memoryStr
	entry.Memory.Topics = topics
	entry.Memory.Kind = kind
	entry.Memory.EventTime = eventTime
	entry.Memory.Participants = normalizeParticipants(participants)
	entry.Memory.Location = strings.TrimSpace(location)
	entry.Memory.LastUpdated = &now
	entry.UpdatedAt = now

	// Generate new ID based on updated content
	newID := generateMemoryID(entry.Memory, appName, userID)

	updated, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal updated memory entry: %w", err)
	}

	query := `UPDATE memories
SET id = ?, memory_data = ?, updated_at = ?
WHERE app_name = ? AND user_id = ? AND id = ? AND deleted_at IS NULL`

	_, err = s.db.ExecContext(ctx, query,
		newID, updated, now.UnixNano(),
		appName, userID, memoryID)
	if err != nil {
		return fmt.Errorf("update memory entry: %w", err)
	}

	return nil
}

// DeleteMemory deletes a memory for a user.
func (s *service) DeleteMemory(ctx context.Context, appName, userID, memoryID string) error {
	if appName == "" {
		return ErrAppNameRequired
	}
	if userID == "" {
		return ErrUserIDRequired
	}
	if memoryID == "" {
		return ErrMemoryIDRequired
	}

	// Use soft delete
	query := `UPDATE memories
SET deleted_at = ?
WHERE app_name = ? AND user_id = ? AND id = ? AND deleted_at IS NULL`

	result, err := s.db.ExecContext(ctx, query,
		time.Now().UnixNano(), appName, userID, memoryID)
	if err != nil {
		return fmt.Errorf("delete memory entry: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("get affected rows: %w", err)
	}
	if rows == 0 {
		return ErrMemoryNotFound
	}

	return nil
}

// ClearMemories clears all memories for a user.
func (s *service) ClearMemories(ctx context.Context, appName, userID string) error {
	if appName == "" {
		return ErrAppNameRequired
	}
	if userID == "" {
		return ErrUserIDRequired
	}

	// Use soft delete
	query := `UPDATE memories
SET deleted_at = ?
WHERE app_name = ? AND user_id = ? AND deleted_at IS NULL`

	_, err := s.db.ExecContext(ctx, query,
		time.Now().UnixNano(), appName, userID)
	if err != nil {
		return fmt.Errorf("clear memories: %w", err)
	}

	return nil
}

// ReadMemories reads memories for a user.
func (s *service) ReadMemories(ctx context.Context, appName, userID string, limit int) ([]*Entry, error) {
	if appName == "" {
		return nil, ErrAppNameRequired
	}
	if userID == "" {
		return nil, ErrUserIDRequired
	}

	if limit <= 0 {
		limit = s.maxSearchResults
	}

	query := `SELECT memory_data FROM memories
WHERE app_name = ? AND user_id = ? AND deleted_at IS NULL
ORDER BY updated_at DESC, created_at DESC
LIMIT ?`

	rows, err := s.db.QueryContext(ctx, query, appName, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("list memories: %w", err)
	}
	defer rows.Close()

	entries := make([]*Entry, 0)
	for rows.Next() {
		var memoryData []byte
		if err := rows.Scan(&memoryData); err != nil {
			return nil, fmt.Errorf("scan memory data: %w", err)
		}

		e := &Entry{}
		if err := json.Unmarshal(memoryData, e); err != nil {
			return nil, fmt.Errorf("unmarshal memory entry: %w", err)
		}
		normalizeEntry(e)
		entries = append(entries, e)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate memories: %w", err)
	}

	return entries, nil
}

// SearchMemories searches memories for a user.
func (s *service) SearchMemories(ctx context.Context, appName, userID, query string, limit int) ([]*Entry, error) {
	if appName == "" {
		return nil, ErrAppNameRequired
	}
	if userID == "" {
		return nil, ErrUserIDRequired
	}

	if limit <= 0 {
		limit = s.maxSearchResults
	}

	// Get all memories for the user
	queryStr := `SELECT memory_data FROM memories
WHERE app_name = ? AND user_id = ? AND deleted_at IS NULL`

	rows, err := s.db.QueryContext(ctx, queryStr, appName, userID)
	if err != nil {
		return nil, fmt.Errorf("search memories: %w", err)
	}
	defer rows.Close()

	entries := make([]*Entry, 0)
	for rows.Next() {
		var memoryData []byte
		if err := rows.Scan(&memoryData); err != nil {
			return nil, fmt.Errorf("scan memory data: %w", err)
		}

		e := &Entry{}
		if err := json.Unmarshal(memoryData, e); err != nil {
			return nil, fmt.Errorf("unmarshal memory entry: %w", err)
		}
		normalizeEntry(e)
		entries = append(entries, e)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate memories: %w", err)
	}

	// Score and rank entries
	return searchEntries(entries, query, s.minSearchScore, limit), nil
}

// getEntry retrieves a single memory entry.
func (s *service) getEntry(ctx context.Context, appName, userID, memoryID string) (*Entry, error) {
	query := `SELECT memory_data FROM memories
WHERE app_name = ? AND user_id = ? AND id = ? AND deleted_at IS NULL`

	var memoryData []byte
	err := s.db.QueryRowContext(ctx, query, appName, userID, memoryID).Scan(&memoryData)
	if err == sql.ErrNoRows {
		return nil, ErrMemoryNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get memory entry: %w", err)
	}

	entry := &Entry{}
	if err := json.Unmarshal(memoryData, entry); err != nil {
		return nil, fmt.Errorf("unmarshal memory entry: %w", err)
	}
	normalizeEntry(entry)
	return entry, nil
}

// enforceMemoryLimit checks if the user has reached the memory limit.
func (s *service) enforceMemoryLimit(ctx context.Context, appName, userID string) error {
	if s.maxMemories <= 0 {
		return nil
	}

	query := `SELECT COUNT(*) FROM memories
WHERE app_name = ? AND user_id = ? AND deleted_at IS NULL`

	var count int
	err := s.db.QueryRowContext(ctx, query, appName, userID).Scan(&count)
	if err != nil {
		return fmt.Errorf("check memory count: %w", err)
	}

	if count >= s.maxMemories {
		return fmt.Errorf("memory limit exceeded for user %s, limit: %d, current: %d",
			userID, s.maxMemories, count)
	}

	return nil
}

// Tools returns the list of available memory tools.
func (s *service) Tools() []fantasy.AgentTool {
	s.toolsMu.Lock()
	defer s.toolsMu.Unlock()

	toolNames := []string{
		AddToolName, UpdateToolName, DeleteToolName,
		ClearToolName, SearchToolName, LoadToolName,
	}

	tools := make([]fantasy.AgentTool, 0)
	for _, name := range toolNames {
		if !s.enabledTools[name] {
			continue
		}
		if _, ok := s.cachedTools[name]; !ok {
			s.cachedTools[name] = s.createTool(name)
		}
		tools = append(tools, s.cachedTools[name])
	}

	slices.SortFunc(tools, func(a, b fantasy.AgentTool) int {
		return strings.Compare(a.Info().Name, b.Info().Name)
	})

	return tools
}

// Close closes the service and releases resources.
func (s *service) Close() error {
	// Database is managed by the caller, don't close it
	return nil
}
