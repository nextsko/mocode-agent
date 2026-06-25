package memory

import (
	"sort"
	"strings"
	"time"
)

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

// GenerateMemoryID returns a stable, deterministic ID for a memory entry.
// The same content+participants always maps to the same ID, which makes memory
// writes idempotent: re-adding an identical memory updates rather than
// duplicates. Episodic fields (kind, event time, participants, location) are
// folded into the ID so distinct events stay distinct.
func GenerateMemoryID(mem *Memory, appName, userID string) string {
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
	return builder.String()
}

// normalizeParticipants trims, deduplicates case-insensitively (keeping the
// original casing), then sorts the result. Cosmetic differences do not
// fragment memory entries. Ported verbatim from the former service.go impl so
// generated IDs stay byte-identical.
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
