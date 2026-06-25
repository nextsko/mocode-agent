package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"charm.land/fantasy"
)

// Context keys for passing memory service and app/user info to tools.
type (
	memoryServiceContextKey struct{}
	appNameContextKey       struct{}
	userIDContextKey        struct{}
)

// WithMemoryServiceInContext adds memory service to context.
func WithMemoryServiceInContext(ctx context.Context, svc Service) context.Context {
	return context.WithValue(ctx, memoryServiceContextKey{}, svc)
}

// WithAppUserInContext adds app name and user ID to context.
func WithAppUserInContext(ctx context.Context, appName, userID string) context.Context {
	ctx = context.WithValue(ctx, appNameContextKey{}, appName)
	ctx = context.WithValue(ctx, userIDContextKey{}, userID)
	return ctx
}

func AppUserFromContext(ctx context.Context) (string, string, bool) {
	appName, ok1 := ctx.Value(appNameContextKey{}).(string)
	userID, ok2 := ctx.Value(userIDContextKey{}).(string)
	return appName, userID, ok1 && ok2
}

// createTool creates a memory tool by name.
func (s *service) createTool(name string) fantasy.AgentTool {
	switch name {
	case AddToolName:
		return s.createAddTool()
	case UpdateToolName:
		return s.createUpdateTool()
	case DeleteToolName:
		return s.createDeleteTool()
	case ClearToolName:
		return s.createClearTool()
	case SearchToolName:
		return s.createSearchTool()
	case LoadToolName:
		return s.createLoadTool()
	default:
		return nil
	}
}

// createAddTool creates the memory_add tool.
func (s *service) createAddTool() fantasy.AgentTool {
	return fantasy.NewAgentTool(
		AddToolName,
		"Add a new memory about the user. Use this tool to store important information about the user's preferences, background, or past interactions.",
		func(ctx context.Context, params AddMemoryRequest, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			svc, err := getMemoryServiceFromContext(ctx)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("memory add tool: %v", err)
			}

			appName, userID, err := getAppUserFromContext(ctx)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("memory add tool: %v", err)
			}

			if params.Memory == "" {
				return fantasy.NewTextErrorResponse("memory content is required"), nil
			}

			topics := params.Topics
			if topics == nil {
				topics = []string{}
			}

			kind := Kind(params.MemoryKind)
			if kind == "" {
				kind = KindFact
			}

			var eventTime *time.Time
			if params.EventTime != "" {
				eventTime = parseFlexibleTime(params.EventTime)
			}

			err = svc.AddMemory(ctx, appName, userID, params.Memory, topics, kind, eventTime, params.Participants, params.Location)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("failed to add memory: %v", err)
			}

			resp := AddMemoryResponse{
				Message: "Memory added successfully",
				Memory:  params.Memory,
				Topics:  topics,
			}
			return responseFromStruct(resp)
		},
	)
}

// createUpdateTool creates the memory_update tool.
func (s *service) createUpdateTool() fantasy.AgentTool {
	return fantasy.NewAgentTool(
		UpdateToolName,
		"Update an existing memory. Use this tool to modify stored information about the user.",
		func(ctx context.Context, params UpdateMemoryRequest, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			svc, err := getMemoryServiceFromContext(ctx)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("memory update tool: %v", err)
			}

			appName, userID, err := getAppUserFromContext(ctx)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("memory update tool: %v", err)
			}

			if params.MemoryID == "" {
				return fantasy.NewTextErrorResponse("memory ID is required"), nil
			}
			if params.Memory == "" {
				return fantasy.NewTextErrorResponse("memory content is required"), nil
			}

			topics := params.Topics
			if topics == nil {
				topics = []string{}
			}

			kind := Kind(params.MemoryKind)
			var eventTime *time.Time
			if params.EventTime != "" {
				eventTime = parseFlexibleTime(params.EventTime)
			}

			err = svc.UpdateMemory(ctx, appName, userID, params.MemoryID, params.Memory, topics, kind, eventTime, params.Participants, params.Location)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("failed to update memory: %v", err)
			}

			resp := UpdateMemoryResponse{
				Message:  "Memory updated successfully",
				MemoryID: params.MemoryID,
				Memory:   params.Memory,
				Topics:   topics,
			}
			return responseFromStruct(resp)
		},
	)
}

// createDeleteTool creates the memory_delete tool.
func (s *service) createDeleteTool() fantasy.AgentTool {
	return fantasy.NewAgentTool(
		DeleteToolName,
		"Delete a specific memory. Use this tool to remove outdated or incorrect information about the user.",
		func(ctx context.Context, params DeleteMemoryRequest, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			svc, err := getMemoryServiceFromContext(ctx)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("memory delete tool: %v", err)
			}

			appName, userID, err := getAppUserFromContext(ctx)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("memory delete tool: %v", err)
			}

			if params.MemoryID == "" {
				return fantasy.NewTextErrorResponse("memory ID is required"), nil
			}

			err = svc.DeleteMemory(ctx, appName, userID, params.MemoryID)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("failed to delete memory: %v", err)
			}

			resp := DeleteMemoryResponse{
				Message:  "Memory deleted successfully",
				MemoryID: params.MemoryID,
			}
			return responseFromStruct(resp)
		},
	)
}

// createClearTool creates the memory_clear tool.
func (s *service) createClearTool() fantasy.AgentTool {
	return fantasy.NewAgentTool(
		ClearToolName,
		"Clear all memories for the user. Use this tool to reset the user's memory completely. This is a dangerous operation!",
		func(ctx context.Context, params ClearMemoryRequest, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			svc, err := getMemoryServiceFromContext(ctx)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("memory clear tool: %v", err)
			}

			appName, userID, err := getAppUserFromContext(ctx)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("memory clear tool: %v", err)
			}

			err = svc.ClearMemories(ctx, appName, userID)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("failed to clear memories: %v", err)
			}

			resp := ClearMemoryResponse{
				Message: "All memories cleared successfully",
			}
			return responseFromStruct(resp)
		},
	)
}

// createSearchTool creates the memory_search tool.
func (s *service) createSearchTool() fantasy.AgentTool {
	return fantasy.NewAgentTool(
		SearchToolName,
		"Search for relevant memories about the user. Returns memories ranked by relevance. Use short keyword-style queries for best results.",
		func(ctx context.Context, params SearchMemoryRequest, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			svc, err := getMemoryServiceFromContext(ctx)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("memory search tool: %v", err)
			}

			appName, userID, err := getAppUserFromContext(ctx)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("memory search tool: %v", err)
			}

			if params.Query == "" {
				resp := SearchMemoryResponse{
					Query:   "",
					Results: []Result{},
					Count:   0,
				}
				return responseFromStruct(resp)
			}

			entries, err := svc.SearchMemories(ctx, appName, userID, params.Query, s.maxSearchResults)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("failed to search memories: %v", err)
			}

			results := make([]Result, len(entries))
			for i, e := range entries {
				results[i] = entryToResult(e)
			}

			resp := SearchMemoryResponse{
				Query:   params.Query,
				Results: results,
				Count:   len(results),
			}
			return responseFromStruct(resp)
		},
	)
}

// createLoadTool creates the memory_load tool.
func (s *service) createLoadTool() fantasy.AgentTool {
	return fantasy.NewAgentTool(
		LoadToolName,
		"Load the most recent memories about the user. Returns memories ordered by last update time. Use this to get a broad overview of what is known about the user.",
		func(ctx context.Context, params LoadMemoryRequest, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			svc, err := getMemoryServiceFromContext(ctx)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("memory load tool: %v", err)
			}

			appName, userID, err := getAppUserFromContext(ctx)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("memory load tool: %v", err)
			}

			limit := params.Limit
			if limit <= 0 {
				limit = 10
			}

			entries, err := svc.ReadMemories(ctx, appName, userID, limit)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("failed to load memories: %v", err)
			}

			results := make([]Result, len(entries))
			for i, e := range entries {
				results[i] = entryToResult(e)
			}

			resp := LoadMemoryResponse{
				Limit:   limit,
				Results: results,
				Count:   len(results),
			}
			return responseFromStruct(resp)
		},
	)
}

// Helper functions

func getMemoryServiceFromContext(ctx context.Context) (Service, error) {
	if svc, ok := ctx.Value(memoryServiceContextKey{}).(Service); ok {
		return svc, nil
	}
	return nil, fmt.Errorf("memory service not found in context")
}

func getAppUserFromContext(ctx context.Context) (string, string, error) {
	appName, ok1 := ctx.Value(appNameContextKey{}).(string)
	userID, ok2 := ctx.Value(userIDContextKey{}).(string)
	if !ok1 || !ok2 {
		return "", "", fmt.Errorf("app name or user ID not found in context")
	}
	return appName, userID, nil
}

func parseFlexibleTime(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}

	for _, layout := range []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
		"2 January 2006",
		"January 2, 2006",
		"Jan 2, 2006",
		"2 Jan 2006",
		"January 2006",
		"Jan 2006",
		"2006-01",
		"2006",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return &t
		}
	}
	return nil
}

func entryToResult(e *Entry) Result {
	r := Result{
		ID:      e.ID,
		Memory:  e.Memory.Memory,
		Topics:  e.Memory.Topics,
		Created: e.CreatedAt,
	}
	if e.Memory.Kind != "" {
		r.Kind = string(e.Memory.Kind)
	}
	if e.Memory.EventTime != nil {
		r.EventTime = e.Memory.EventTime.Format(time.RFC3339)
	}
	if len(e.Memory.Participants) > 0 {
		r.Participants = e.Memory.Participants
	}
	if e.Memory.Location != "" {
		r.Location = e.Memory.Location
	}
	r.Score = e.Score
	return r
}

// responseFromStruct converts a struct to a ToolResponse as JSON text.
func responseFromStruct(v any) (fantasy.ToolResponse, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return fantasy.ToolResponse{}, fmt.Errorf("marshal response: %w", err)
	}
	return fantasy.NewTextResponse(string(data)), nil
}
