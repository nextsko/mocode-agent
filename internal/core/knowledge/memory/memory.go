// Package memory implements the agent memory service and its LLM tools.
//
// Pure value types (Kind, Memory, Entry, the request/response DTOs, sentinel
// errors, and the deterministic ID generator) live in
// internal/domain/memory, which has no upward dependencies. This package
// holds the Service interface and its implementation plus the tool wiring.
// The domain types are re-exported here as type aliases so existing callers
// keep importing a single "memory" package without churn.
package memory

import (
	"context"
	"time"

	"charm.land/fantasy"

	dmem "github.com/nextsko/mocode-agent/internal/domain/memory"
)

// Re-exported domain types. Callers use memory.Entry / memory.Memory / etc.
// unchanged; the definitions live in internal/domain/memory.
type (
	// Kind distinguishes between semantic facts and episodic memories.
	Kind = dmem.Kind
	// Memory represents a memory entry with content and metadata.
	Memory = dmem.Memory
	// Entry represents a memory entry stored in the system.
	Entry = dmem.Entry
	// Result represents a single memory result for tool responses.
	Result = dmem.Result
	// AddMemoryRequest is the input for the add memory tool.
	AddMemoryRequest = dmem.AddMemoryRequest
	// AddMemoryResponse is the response from the add memory tool.
	AddMemoryResponse = dmem.AddMemoryResponse
	// UpdateMemoryRequest is the input for the update memory tool.
	UpdateMemoryRequest = dmem.UpdateMemoryRequest
	// UpdateMemoryResponse is the response from the update memory tool.
	UpdateMemoryResponse = dmem.UpdateMemoryResponse
	// DeleteMemoryRequest is the input for the delete memory tool.
	DeleteMemoryRequest = dmem.DeleteMemoryRequest
	// DeleteMemoryResponse is the response from the delete memory tool.
	DeleteMemoryResponse = dmem.DeleteMemoryResponse
	// ClearMemoryRequest is the input for the clear memory tool.
	ClearMemoryRequest = dmem.ClearMemoryRequest
	// ClearMemoryResponse is the response from the clear memory tool.
	ClearMemoryResponse = dmem.ClearMemoryResponse
	// SearchMemoryRequest is the input for the search memory tool.
	SearchMemoryRequest = dmem.SearchMemoryRequest
	// SearchMemoryResponse is the response from the search memory tool.
	SearchMemoryResponse = dmem.SearchMemoryResponse
	// LoadMemoryRequest is the input for the load memory tool.
	LoadMemoryRequest = dmem.LoadMemoryRequest
	// LoadMemoryResponse is the response from the load memory tool.
	LoadMemoryResponse = dmem.LoadMemoryResponse
)

// Re-exported sentinel errors.
var (
	ErrAppNameRequired  = dmem.ErrAppNameRequired
	ErrUserIDRequired   = dmem.ErrUserIDRequired
	ErrMemoryIDRequired = dmem.ErrMemoryIDRequired
	ErrMemoryRequired   = dmem.ErrMemoryRequired
	ErrMemoryNotFound   = dmem.ErrMemoryNotFound
	ErrUserNotFound     = dmem.ErrUserNotFound
)

// Re-exported domain constants.
const (
	KindFact    = dmem.KindFact
	KindEpisode = dmem.KindEpisode
)

// GenerateMemoryID delegates to the domain implementation so generated IDs
// stay stable and are shared with the persistence layer.
func GenerateMemoryID(mem *Memory, appName, userID string) string {
	return dmem.GenerateMemoryID(mem, appName, userID)
}

// Tool names for memory tools.
const (
	AddToolName    = "memory_add"
	UpdateToolName = "memory_update"
	DeleteToolName = "memory_delete"
	ClearToolName  = "memory_clear"
	SearchToolName = "memory_search"
	LoadToolName   = "memory_load"
)

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
