package agenttools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/core/tools/contract"
	"github.com/nextsko/mocode-agent/internal/domain/session"
	"github.com/nextsko/mocode-agent/internal/domain/session/message"
	"github.com/nextsko/mocode-agent/internal/domain/session/sessionexport"
)

// ─── think (port) ─────────────────────────────────────────────────────────────

// NewThinkPortTool is the framework-agnostic form of the think tool.
func NewThinkPortTool() contract.Tool {
	return contract.NewFunc(ThinkToolName, toolutil.FirstLineDescription(thinkDescription), map[string]any{
		"type": "object",
		"properties": map[string]any{
			"thought": map[string]any{
				"type":        "string",
				"description": "The reasoning or analysis to record before acting",
			},
		},
		"required": []string{"thought"},
	}, func(_ context.Context, _ contract.ToolContext, args json.RawMessage) (contract.ToolResult, error) {
		var params ThinkParams
		if err := json.Unmarshal(args, &params); err != nil {
			return contract.ToolResult{}, fmt.Errorf("invalid parameters: %w", err)
		}
		return contract.ToolResult{
			Content:  params.Thought,
			Metadata: map[string]any{"thought": params.Thought},
		}, nil
	})
}

// ─── todos (port) ─────────────────────────────────────────────────────────────

// NewTodosPortTool is the framework-agnostic form of the todos tool.
func NewTodosPortTool(sessions session.Service) contract.Tool {
	return contract.NewFunc(TodosToolName, toolutil.FirstLineDescription(todosDescription), map[string]any{
		"type": "object",
		"properties": map[string]any{
			"todos": map[string]any{
				"type":        "array",
				"description": "The updated todo list",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"content": map[string]any{
							"type":        "string",
							"description": "What needs to be done (imperative form)",
						},
						"status": map[string]any{
							"type":        "string",
							"description": "Task status: pending, in_progress, or completed",
						},
						"active_form": map[string]any{
							"type":        "string",
							"description": "Present continuous form (e.g., 'Running tests')",
						},
					},
				},
			},
		},
		"required": []string{"todos"},
	}, func(ctx context.Context, _ contract.ToolContext, args json.RawMessage) (contract.ToolResult, error) {
		var params TodosParams
		if err := json.Unmarshal(args, &params); err != nil {
			return contract.ToolResult{}, fmt.Errorf("invalid parameters: %w", err)
		}
		text, meta, err := runTodos(ctx, sessions, params)
		if err != nil {
			return contract.ToolResult{}, err
		}
		return contract.ToolResult{Content: text, Metadata: map[string]any{"result": meta}}, nil
	})
}

// runTodos applies an update to the session's todo list and renders the text
// summary. Shared by the legacy fantasy constructor and the port.
func runTodos(ctx context.Context, sessions session.Service, params TodosParams) (string, TodosResponseMetadata, error) {
	sessionID := toolutil.GetSessionFromContext(ctx)
	if sessionID == "" {
		return "", TodosResponseMetadata{}, fmt.Errorf("session ID is required for managing todos")
	}

	currentSession, err := sessions.Get(ctx, sessionID)
	if err != nil {
		return "", TodosResponseMetadata{}, fmt.Errorf("failed to get session: %w", err)
	}

	isNew := len(currentSession.Todos) == 0
	oldStatusByContent := make(map[string]session.TodoStatus)
	for _, todo := range currentSession.Todos {
		oldStatusByContent[todo.Content] = todo.Status
	}

	for _, item := range params.Todos {
		switch item.Status {
		case "pending", "in_progress", "completed":
		default:
			return "", TodosResponseMetadata{}, fmt.Errorf("invalid status %q for todo %q", item.Status, item.Content)
		}
	}

	todos := make([]session.Todo, len(params.Todos))
	var justCompleted []string
	var justStarted string
	completedCount := 0

	for i, item := range params.Todos {
		todos[i] = session.Todo{
			Content:    item.Content,
			Status:     session.TodoStatus(item.Status),
			ActiveForm: item.ActiveForm,
		}

		newStatus := session.TodoStatus(item.Status)
		oldStatus, existed := oldStatusByContent[item.Content]

		if newStatus == session.TodoStatusCompleted {
			completedCount++
			if existed && oldStatus != session.TodoStatusCompleted {
				justCompleted = append(justCompleted, item.Content)
			}
		}

		if newStatus == session.TodoStatusInProgress {
			if !existed || oldStatus != session.TodoStatusInProgress {
				if item.ActiveForm != "" {
					justStarted = item.ActiveForm
				} else {
					justStarted = item.Content
				}
			}
		}
	}

	currentSession.Todos = todos
	if _, err := sessions.Save(ctx, currentSession); err != nil {
		return "", TodosResponseMetadata{}, fmt.Errorf("failed to save todos: %w", err)
	}

	response := "Todo list updated successfully.\n\n"

	pendingCount := 0
	inProgressCount := 0
	for _, todo := range todos {
		switch todo.Status {
		case session.TodoStatusPending:
			pendingCount++
		case session.TodoStatusInProgress:
			inProgressCount++
		}
	}

	response += fmt.Sprintf("Status: %d pending, %d in progress, %d completed\n", pendingCount, inProgressCount, completedCount)
	response += "Todos have been modified successfully. Ensure that you continue to use the todo list to track your progress. Please proceed with the current tasks if applicable."

	return response, TodosResponseMetadata{
		IsNew:         isNew,
		Todos:         todos,
		JustCompleted: justCompleted,
		JustStarted:   justStarted,
		Completed:     completedCount,
		Total:         len(todos),
	}, nil
}

// ─── session_export (port) ───────────────────────────────────────────────────

// NewSessionExportPortTool is the framework-agnostic form of session_export.
func NewSessionExportPortTool(messages message.Service, workingDir string) contract.Tool {
	return contract.NewFunc(SessionExportToolName, "Export the current session transcript to a local Markdown or HTML file.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"format": map[string]any{
				"type":        "string",
				"description": "Export format: markdown, md, obsidian-md, or html. Defaults to markdown",
			},
			"scope": map[string]any{
				"type":        "string",
				"description": "Export scope: all or recent10. Defaults to all",
			},
		},
	}, func(ctx context.Context, _ contract.ToolContext, args json.RawMessage) (contract.ToolResult, error) {
		var params SessionExportParams
		if err := json.Unmarshal(args, &params); err != nil {
			return contract.ToolResult{}, fmt.Errorf("invalid parameters: %w", err)
		}
		text, meta, err := runSessionExport(ctx, messages, workingDir, params)
		if err != nil {
			return contract.ToolResult{Error: err}, nil
		}
		return contract.ToolResult{Content: text, Metadata: map[string]any{"result": meta}}, nil
	})
}

// runSessionExport writes the current session transcript and reports the path.
func runSessionExport(ctx context.Context, messages message.Service, workingDir string, params SessionExportParams) (string, any, error) {
	sessionID := toolutil.GetSessionFromContext(ctx)
	if sessionID == "" {
		return "", nil, fmt.Errorf("session ID is required for export")
	}
	format := strings.TrimSpace(params.Format)
	if format == "" {
		format = "markdown"
	}
	scope := strings.TrimSpace(params.Scope)
	if scope == "" {
		scope = "all"
	}

	msgs, err := messages.List(ctx, sessionID)
	if err != nil {
		return "", nil, fmt.Errorf("list messages: %w", err)
	}
	result, err := sessionexport.Export(msgs, sessionexport.Options{
		SessionID:  sessionID,
		Format:     format,
		Scope:      scope,
		WorkingDir: workingDir,
		Now:        time.Now(),
	})
	if err != nil {
		return "", nil, err
	}
	return fmt.Sprintf("Session exported to %s", result.Path), result, nil
}
