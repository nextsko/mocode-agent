package agenttools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"

	"charm.land/fantasy"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/core/tools/contract"
	"github.com/nextsko/mocode-agent/internal/store"
)

//go:embed session_search.md
var sessionSearchDescription []byte

const SessionSearchToolName = "session_search"

type SessionSearchParams struct {
	Query           string `json:"query,omitempty" description:"Search query for full-text search across all session messages"`
	SessionID       string `json:"session_id,omitempty" description:"Session ID to scroll within (use with around_message_id)"`
	AroundMessageID string `json:"around_message_id,omitempty" description:"Anchor message ID to scroll around (use with session_id)"`
	Window          int    `json:"window,omitempty" description:"Number of messages before/after anchor for scroll mode (default 5)"`
	Limit           int    `json:"limit,omitempty" description:"Maximum number of results (default 10 for search, 20 for browse)"`
}

// NewSessionSearchTool is the legacy fantasy-typed constructor. New code should
// use NewSessionSearchPortTool and wrap it at the agent boundary.
func NewSessionSearchTool(se *store.SessionSearch) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		SessionSearchToolName,
		toolutil.FirstLineDescription(sessionSearchDescription),
		func(ctx context.Context, params SessionSearchParams, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			text, meta, err := runSessionSearch(ctx, se, params)
			if err != nil {
				return fantasy.ToolResponse{}, err
			}
			return fantasy.WithResponseMetadata(fantasy.NewTextResponse(text), meta), nil
		})
}

// NewSessionSearchPortTool is the framework-agnostic contract.Tool form of the
// session search tool. It is the pilot migration for the toolkit port and is
// wrapped by core/agent/agenttooladapter in production.
func NewSessionSearchPortTool(se *store.SessionSearch) contract.Tool {
	return &sessionSearchPort{se: se}
}

// sessionSearchPort implements contract.Tool for session search.
type sessionSearchPort struct {
	se *store.SessionSearch
}

func (t *sessionSearchPort) Name() string { return SessionSearchToolName }

func (t *sessionSearchPort) Description() string {
	return toolutil.FirstLineDescription(sessionSearchDescription)
}

func (t *sessionSearchPort) Schema() contract.Schema {
	return contract.Schema{
		Name:        SessionSearchToolName,
		Description: t.Description(),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Search query for full-text search across all session messages",
				},
				"session_id": map[string]any{
					"type":        "string",
					"description": "Session ID to scroll within (use with around_message_id)",
				},
				"around_message_id": map[string]any{
					"type":        "string",
					"description": "Anchor message ID to scroll around (use with session_id)",
				},
				"window": map[string]any{
					"type":        "integer",
					"description": "Number of messages before/after anchor for scroll mode (default 5)",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum number of results (default 10 for search, 20 for browse)",
				},
			},
		},
	}
}

func (t *sessionSearchPort) Execute(ctx context.Context, _ contract.ToolContext, args json.RawMessage) (contract.ToolResult, error) {
	var params SessionSearchParams
	if len(args) > 0 {
		if err := json.Unmarshal(args, &params); err != nil {
			return contract.ToolResult{}, fmt.Errorf("invalid parameters: %w", err)
		}
	}

	text, meta, err := runSessionSearch(ctx, t.se, params)
	if err != nil {
		return contract.ToolResult{}, err
	}
	return contract.ToolResult{
		Content:  text,
		Metadata: map[string]any{"response": meta},
	}, nil
}

// runSessionSearch dispatches between discovery, scroll, and browse and returns
// the rendered text plus the structured payload (used as response metadata by
// both the fantasy wrapper and the port implementation).
func runSessionSearch(ctx context.Context, se *store.SessionSearch, params SessionSearchParams) (string, any, error) {
	switch {
	case params.Query != "":
		return searchText(ctx, se, params)
	case params.SessionID != "" && params.AroundMessageID != "":
		return scrollText(ctx, se, params)
	default:
		return browseText(ctx, se, params)
	}
}

func searchText(ctx context.Context, se *store.SessionSearch, params SessionSearchParams) (string, any, error) {
	resp, err := se.Search(ctx, params.Query, params.Limit)
	if err != nil {
		return "", nil, fmt.Errorf("session search failed: %w", err)
	}
	if resp.Total == 0 {
		return fmt.Sprintf("No sessions found matching query %q.", params.Query), nil, nil
	}

	result := fmt.Sprintf("Found %d session(s) matching %q:\n\n", resp.Total, params.Query)
	for _, r := range resp.Results {
		title := r.SessionTitle
		if title == "" {
			title = "(untitled)"
		}
		result += fmt.Sprintf("Session: %s [%s]\n", title, r.SessionID)
		if r.Snippet != "" {
			result += fmt.Sprintf("  Match: %s\n", r.Snippet)
		}
		result += fmt.Sprintf("  Role: %s | Time: %d\n\n", r.Role, r.Timestamp)
	}
	return result, resp, nil
}

func scrollText(ctx context.Context, se *store.SessionSearch, params SessionSearchParams) (string, any, error) {
	resp, err := se.Scroll(ctx, params.SessionID, params.AroundMessageID, params.Window)
	if err != nil {
		return "", nil, fmt.Errorf("session scroll failed: %w", err)
	}

	result := fmt.Sprintf("Session %s around message %s (window=%d):\n\n", params.SessionID, params.AroundMessageID, params.Window)
	for _, r := range resp.Results {
		prefix := "  "
		if r.Snippet != "" && len(r.Snippet) > 9 && r.Snippet[:9] == "[anchor] " {
			prefix = ">> "
		}
		result += fmt.Sprintf("%s[%s] %s: %s\n", prefix, r.Role, r.MessageID, truncateToolText(r.Content, 200))
	}
	return result, resp, nil
}

func browseText(ctx context.Context, se *store.SessionSearch, params SessionSearchParams) (string, any, error) {
	resp, err := se.Browse(ctx, params.Limit)
	if err != nil {
		return "", nil, fmt.Errorf("session browse failed: %w", err)
	}
	if resp.Total == 0 {
		return "No sessions found. Start a conversation to create sessions.", nil, nil
	}

	result := fmt.Sprintf("Recent sessions (%d total):\n\n", resp.Total)
	for _, s := range resp.Sessions {
		title := s.Title
		if title == "" {
			title = "(untitled)"
		}
		result += fmt.Sprintf("  [%s] %s (updated: %d, messages: %d)\n", s.ID, title, s.UpdatedAt, s.MessageCount)
	}
	return result, resp, nil
}

func truncateToolText(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
