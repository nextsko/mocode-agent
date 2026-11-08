package agenttools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/core/config"
	coreplan "github.com/nextsko/mocode-agent/internal/core/plan"
	"github.com/nextsko/mocode-agent/internal/core/skills"
	"github.com/nextsko/mocode-agent/internal/core/tools/contract"
	"github.com/nextsko/mocode-agent/internal/core/tools/internalx/lsp"
	"github.com/nextsko/mocode-agent/internal/domain/session"
	"github.com/nextsko/mocode-agent/internal/domain/session/message"
	"github.com/nextsko/mocode-agent/internal/domain/session/sessionexport"
)

// Shared JSON-schema and response-metadata keys for the port tools.
const (
	schemaActionKey = "action"
	schemaEnumKey   = "enum"
	metaResultKey   = "result"
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
		return contract.ToolResult{Content: text, Metadata: map[string]any{metaResultKey: meta}}, nil
	})
}

// runTodos applies an update to the session's todo list and renders the text
// summary.
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

	// Mirror the todo list into the observable plan store so transports
	// (ACP, future UIs) render the same plan the agent maintains.
	planItems := make([]coreplan.Item, 0, len(todos))
	for i, todo := range todos {
		planItems = append(planItems, coreplan.Item{
			ID:       fmt.Sprintf("todo-%d", i+1),
			Content:  todo.Content,
			Status:   coreplan.Status(todo.Status),
			Priority: "medium",
		})
	}
	coreplan.Default().Set(sessionID, planItems)

	response := "Todo list updated successfully.\n\n"

	pendingCount := 0
	inProgressCount := 0
	for _, todo := range todos {
		switch todo.Status {
		case session.TodoStatusPending:
			pendingCount++
		case session.TodoStatusInProgress:
			inProgressCount++
		default:
			// Completed todos are counted above; other statuses add nothing.
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
			return contract.ToolResult{Error: err}, nil //nolint:nilerr // Tool-level failures are reported in the result, not as a Go error.
		}
		return contract.ToolResult{Content: text, Metadata: map[string]any{metaResultKey: meta}}, nil
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

// ─── diagnostics (port) ──────────────────────────────────────────────────────

// NewDiagnosticsPortTool is the framework-agnostic form of lsp_diagnostics.
func NewDiagnosticsPortTool(lspManager *lsp.Manager) contract.Tool {
	return contract.NewFunc(DiagnosticsToolName, toolutil.FirstLineDescription(diagnosticsDescription), map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "The path to the file to get diagnostics for (leave empty for project diagnostics)",
			},
		},
	}, func(ctx context.Context, _ contract.ToolContext, args json.RawMessage) (contract.ToolResult, error) {
		var params DiagnosticsParams
		if len(args) > 0 {
			if err := json.Unmarshal(args, &params); err != nil {
				return contract.ToolResult{}, fmt.Errorf("invalid parameters: %w", err)
			}
		}
		out, err := runDiagnostics(ctx, lspManager, params)
		if err != nil {
			return contract.ToolResult{Error: err}, nil //nolint:nilerr // Tool-level failures are reported in the result, not as a Go error.
		}
		return contract.ToolResult{Content: out}, nil
	})
}

// runDiagnostics refreshes LSP diagnostics and renders them.
func runDiagnostics(ctx context.Context, lspManager *lsp.Manager, params DiagnosticsParams) (string, error) {
	if lspManager.Clients().Len() == 0 {
		return "", fmt.Errorf("no LSP clients available")
	}
	lsp.NotifyLSPs(ctx, lspManager, params.FilePath)
	return lsp.GetDiagnostics(params.FilePath, lspManager), nil
}

// ─── message_export (port) ───────────────────────────────────────────────────

// NewMessageExportPortTool is the framework-agnostic form of message_export.
func NewMessageExportPortTool(messages message.Service, workingDir string) contract.Tool {
	return contract.NewFunc(MessageExportToolName, "Export selected messages to .mocode/export/recents/<dir>_<time>.md. If no message_ids given, exports the last assistant reply.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"message_ids": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Specific message IDs to export. If empty, exports the last assistant reply.",
			},
			"format": map[string]any{
				"type":        "string",
				"description": "Export format: markdown, md, or obsidian-md. Defaults to markdown",
			},
		},
	}, func(ctx context.Context, _ contract.ToolContext, args json.RawMessage) (contract.ToolResult, error) {
		var params MessageExportParams
		if len(args) > 0 {
			if err := json.Unmarshal(args, &params); err != nil {
				return contract.ToolResult{}, fmt.Errorf("invalid parameters: %w", err)
			}
		}
		text, meta, err := runMessageExport(ctx, messages, workingDir, params)
		if err != nil {
			return contract.ToolResult{Error: err}, nil //nolint:nilerr // Tool-level failures are reported in the result, not as a Go error.
		}
		return contract.ToolResult{Content: text, Metadata: map[string]any{metaResultKey: meta}}, nil
	})
}

// runMessageExport writes selected messages to the recents export directory.
func runMessageExport(ctx context.Context, messages message.Service, workingDir string, params MessageExportParams) (string, any, error) {
	sessionID := toolutil.GetSessionFromContext(ctx)
	if sessionID == "" {
		return "", nil, fmt.Errorf("session ID is required for export")
	}

	format := strings.TrimSpace(params.Format)
	if format == "" {
		format = "markdown"
	}

	msgs, err := selectExportMessages(ctx, messages, sessionID, params.MessageIDs)
	if err != nil {
		return "", nil, err
	}

	result, err := sessionexport.ExportRecents(sessionexport.RecentExportOptions{
		Messages:   msgs,
		WorkingDir: workingDir,
		Format:     format,
		Now:        time.Now(),
	})
	if err != nil {
		return "", nil, err
	}
	return fmt.Sprintf("Exported %d message(s) to %s", len(msgs), result.Path), result, nil
}

// selectExportMessages returns the explicitly requested messages, or the most
// recent assistant reply when no IDs are given.
func selectExportMessages(ctx context.Context, messages message.Service, sessionID string, ids []string) ([]message.Message, error) {
	if len(ids) > 0 {
		msgs := make([]message.Message, 0, len(ids))
		for _, id := range ids {
			msg, err := messages.Get(ctx, id)
			if err != nil {
				return nil, fmt.Errorf("get message %s: %w", id, err)
			}
			msgs = append(msgs, msg)
		}
		return msgs, nil
	}

	all, err := messages.List(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	for i := len(all) - 1; i >= 0; i-- {
		if all[i].Role == message.Assistant && all[i].Content().Text != "" {
			return []message.Message{all[i]}, nil
		}
	}
	return nil, fmt.Errorf("no assistant reply found to export")
}

// ─── mocode_info (port) ──────────────────────────────────────────────────────

// NewMocodeInfoPortTool is the framework-agnostic form of the mocode_info tool.
func NewMocodeInfoPortTool(
	cfg *config.ConfigStore,
	lspManager *lsp.Manager,
	allSkills []*skills.Skill,
	activeSkills []*skills.Skill,
	skillTracker *skills.Tracker,
) contract.Tool {
	return contract.NewFunc(MocodeInfoToolName, string(mocodeInfoDescription), map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}, func(_ context.Context, _ contract.ToolContext, _ json.RawMessage) (contract.ToolResult, error) {
		return contract.ToolResult{Content: buildMocodeInfo(cfg, lspManager, allSkills, activeSkills, skillTracker)}, nil
	})
}

// ─── mocode_logs (port) ──────────────────────────────────────────────────────

// NewMocodeLogsPortTool is the framework-agnostic form of the mocode_logs tool.
func NewMocodeLogsPortTool(logFile string) contract.Tool {
	return contract.NewFunc(MocodeLogsToolName, string(mocodeLogsDescription), map[string]any{
		"type": "object",
		"properties": map[string]any{
			"lines": map[string]any{
				"type":        "integer",
				"description": "Number of log lines to read from the end of the log file",
			},
		},
	}, func(_ context.Context, _ contract.ToolContext, args json.RawMessage) (contract.ToolResult, error) {
		var params MocodeLogsParams
		if len(args) > 0 {
			if err := json.Unmarshal(args, &params); err != nil {
				return contract.ToolResult{}, fmt.Errorf("invalid parameters: %w", err)
			}
		}
		return contract.ToolResult{Content: runMocodeLogs(logFile, params)}, nil
	})
}

// ─── memory (port) ───────────────────────────────────────────────────────────

// NewMemoryPortTool is the framework-agnostic form of the memory tool.
func NewMemoryPortTool(deps MemoryDeps) contract.Tool {
	deps = normalizeMemoryDeps(deps)
	return contract.NewFunc(MemoryToolName, toolutil.FirstLineDescription(memoryDescription), map[string]any{
		"type": "object",
		"properties": map[string]any{
			schemaActionKey: map[string]any{
				"type":        "string",
				schemaEnumKey: []string{"write", "list", "search", "extract", "forget"},
				"description": "write stores an entry; list and search browse them; extract promotes a session memory into the durable global tier; forget archives one.",
			},
			"title":   map[string]any{"type": "string", "description": "For action=write: a one-line title."},
			"body":    map[string]any{"type": "string", "description": "For action=write: the markdown content."},
			"scope":   map[string]any{"type": "string", schemaEnumKey: []string{"session", "global"}, "description": "For action=write: session (default) keeps it with this session; global makes it durable."},
			"kind":    map[string]any{"type": "string", schemaEnumKey: []string{"note", "lesson", "decision", "fact"}, "description": "For action=write or extract: what sort of memory this is."},
			"tags":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Free-form labels to make the entry findable later."},
			"path":    map[string]any{"type": "string", "description": "Root-relative path of an entry. Required for extract and forget."},
			"query":   map[string]any{"type": "string", "description": "For action=search: the text to look for."},
			"session": map[string]any{"type": "string", "description": "For action=list, search or extract: restrict to one session."},
			"limit":   map[string]any{"type": "integer", "description": "Maximum entries to return."},
		},
		"required": []string{schemaActionKey},
	}, func(ctx context.Context, _ contract.ToolContext, args json.RawMessage) (contract.ToolResult, error) {
		var params MemoryParams
		if err := json.Unmarshal(args, &params); err != nil {
			return contract.ToolResult{}, fmt.Errorf("invalid parameters: %w", err)
		}
		out, err := runMemory(ctx, deps, params)
		if err != nil {
			return contract.ToolResult{}, err
		}
		return outcomeToResult(out), nil
	})
}

// ─── learn (port) ────────────────────────────────────────────────────────────

// NewLearnPortTool is the framework-agnostic form of the learn tool.
func NewLearnPortTool(deps LearnDeps) contract.Tool {
	return contract.NewFunc(LearnToolName, toolutil.FirstLineDescription(learnDescription), map[string]any{
		"type": "object",
		"properties": map[string]any{
			schemaActionKey: map[string]any{
				"type":        "string",
				schemaEnumKey: []string{"create", "refine", "list"},
				"description": "create writes a new skill, refine rewrites an existing one, list surveys what already exists",
			},
			"name":         map[string]any{"type": "string", "description": "Skill name in kebab-case. Required for create and refine."},
			"description":  map[string]any{"type": "string", "description": "One-line trigger description starting with 'Use when'."},
			"instructions": map[string]any{"type": "string", "description": "The full markdown body of SKILL.md."},
			"reason":       map[string]any{"type": "string", "description": "One sentence on why this is worth remembering."},
		},
		"required": []string{schemaActionKey},
	}, func(ctx context.Context, _ contract.ToolContext, args json.RawMessage) (contract.ToolResult, error) {
		var params LearnParams
		if err := json.Unmarshal(args, &params); err != nil {
			return contract.ToolResult{}, fmt.Errorf("invalid parameters: %w", err)
		}
		callID, _ := toolutil.ToolCallIDFromCtx(ctx)
		out, err := runLearn(ctx, deps, params, callID)
		if err != nil {
			return contract.ToolResult{}, err
		}
		return outcomeToResult(out), nil
	})
}

// outcomeToResult converts an internal outcome into the port result.
func outcomeToResult(o toolOutcome) contract.ToolResult {
	res := contract.ToolResult{StopTurn: o.StopTurn}
	if o.Err {
		res.Error = fmt.Errorf("%s", o.Text)
	} else {
		res.Content = o.Text
	}
	if o.Meta != nil {
		res.Metadata = map[string]any{metaResultKey: o.Meta}
	}
	return res
}
