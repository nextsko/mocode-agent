// Package memory provides memory service for Mocode agent.
package memory

import (
	"context"
	"time"

	"charm.land/fantasy"
)

// Tool names for memory tools.
const (
	AddToolName    = "memory_add"
	UpdateToolName = "memory_update"
	DeleteToolName = "memory_delete"
	ClearToolName  = "memory_clear"
	SearchToolName = "memory_search"
	LoadToolName   = "memory_load"
)

// GenerateMemoryID returns a stable ID for a memory entry.
func GenerateMemoryID(mem *Memory, appName, userID string) string {
	return generateMemoryID(mem, appName, userID)
}

// Kind distinguishes between semantic facts and episodic memories.
type Kind string

const (
	// KindFact represents stable personal attributes, preferences, or background.
	// Example: "User is a software engineer."
	KindFact Kind = "fact"
	// KindEpisode represents a specific event that happened at a particular time.
	// Example: "On 2024-05-07, User went hiking at Mt. Fuji with Alice."
	KindEpisode Kind = "episode"
)

// Memory represents a memory entry with content and metadata.
type Memory struct {
	Memory      string     `json:"memory"`                 // Memory content.
	Topics      []string   `json:"topics,omitempty"`       // Memory topics (array).
	LastUpdated *time.Time `json:"last_updated,omitempty"` // Last update time.

	// Episodic memory fields.
	Kind         Kind       `json:"kind,omitempty"`         // Memory kind: "fact" or "episode".
	EventTime    *time.Time `json:"event_time,omitempty"`   // When the event occurred.
	Participants []string   `json:"participants,omitempty"` // People involved in the event.
	Location     string     `json:"location,omitempty"`     // Where the event took place.
}

// Entry represents a memory entry stored in the system.
type Entry struct {
	ID        string    `json:"id"`              // ID is the unique identifier of the memory.
	AppName   string    `json:"app_name"`        // App name is the name of the application.
	UserID    string    `json:"user_id"`         // User ID is the unique identifier of the user.
	Memory    *Memory   `json:"memory"`          // Memory is the memory content.
	CreatedAt time.Time `json:"created_at"`      // CreatedAt is the creation time.
	UpdatedAt time.Time `json:"updated_at"`      // UpdatedAt is the last update time.
	Score     float64   `json:"score,omitempty"` // Score is the similarity score from search (0-1).
}

// Service defines the interface for memory service operations.
type Service interface {
	// AddMemory adds or updates a memory for a user (idempotent).
	AddMemory(ctx context.Context, appName, userID, memory string,
		topics []string, kind Kind, eventTime *time.Time,
		participants []string, location string) error

	// UpdateMemory updates an existing memory for a user.
	UpdateMemory(ctx context.Context, appName, userID, memoryID, memory string,
		topics []string, kind Kind, eventTime *time.Time,
		participants []string, location string) error

	// DeleteMemory deletes a memory for a user.
	DeleteMemory(ctx context.Context, appName, userID, memoryID string) error

	// ClearMemories clears all memories for a user.
	ClearMemories(ctx context.Context, appName, userID string) error

	// ReadMemories reads memories for a user.
	ReadMemories(ctx context.Context, appName, userID string, limit int) ([]*Entry, error)

	// SearchMemories searches memories for a user.
	SearchMemories(ctx context.Context, appName, userID, query string, limit int) ([]*Entry, error)

	// Tools returns the list of available memory tools.
	Tools() []fantasy.AgentTool

	// Close closes the service and releases resources.
	Close() error
}
