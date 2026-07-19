package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"

	"charm.land/fantasy"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
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

func NewSessionSearchTool(se *store.SessionSearch) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		SessionSearchToolName,
		toolutil.FirstLineDescription(sessionSearchDescription),
		func(ctx context.Context, params SessionSearchParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			switch {
			case params.Query != "":
				return handleDiscovery(ctx, se, params)
			case params.SessionID != "" && params.AroundMessageID != "":
				return handleScroll(ctx, se, params)
			default:
				return handleBrowse(ctx, se, params)
			}
		})
}

func handleDiscovery(ctx context.Context, se *store.SessionSearch, params SessionSearchParams) (fantasy.ToolResponse, error) {
	resp, err := se.Search(ctx, params.Query, params.Limit)
	if err != nil {
		return fantasy.ToolResponse{}, fmt.Errorf("session search failed: %w", err)
	}
	if resp.Total == 0 {
		return fantasy.NewTextResponse(fmt.Sprintf("No sessions found matching query %q.", params.Query)), nil
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

	data, _ := json.Marshal(resp)
	return fantasy.WithResponseMetadata(fantasy.NewTextResponse(result), data), nil
}

func handleScroll(ctx context.Context, se *store.SessionSearch, params SessionSearchParams) (fantasy.ToolResponse, error) {
	resp, err := se.Scroll(ctx, params.SessionID, params.AroundMessageID, params.Window)
	if err != nil {
		return fantasy.ToolResponse{}, fmt.Errorf("session scroll failed: %w", err)
	}

	result := fmt.Sprintf("Session %s around message %s (window=%d):\n\n", params.SessionID, params.AroundMessageID, params.Window)
	for _, r := range resp.Results {
		prefix := "  "
		if r.Snippet != "" && len(r.Snippet) > 9 && r.Snippet[:9] == "[anchor] " {
			prefix = ">> "
		}
		result += fmt.Sprintf("%s[%s] %s: %s\n", prefix, r.Role, r.MessageID, truncateToolText(r.Content, 200))
	}

	data, _ := json.Marshal(resp)
	return fantasy.WithResponseMetadata(fantasy.NewTextResponse(result), data), nil
}

func handleBrowse(ctx context.Context, se *store.SessionSearch, params SessionSearchParams) (fantasy.ToolResponse, error) {
	resp, err := se.Browse(ctx, params.Limit)
	if err != nil {
		return fantasy.ToolResponse{}, fmt.Errorf("session browse failed: %w", err)
	}

	if resp.Total == 0 {
		return fantasy.NewTextResponse("No sessions found. Start a conversation to create sessions."), nil
	}

	result := fmt.Sprintf("Recent sessions (%d total):\n\n", resp.Total)
	for _, s := range resp.Sessions {
		title := s.Title
		if title == "" {
			title = "(untitled)"
		}
		result += fmt.Sprintf("  [%s] %s (updated: %d, messages: %d)\n", s.ID, title, s.UpdatedAt, s.MessageCount)
	}

	data, _ := json.Marshal(resp)
	return fantasy.WithResponseMetadata(fantasy.NewTextResponse(result), data), nil
}

func truncateToolText(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
