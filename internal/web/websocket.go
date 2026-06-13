package web

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/package-register/mocode/internal/agent/notify"
	agentexec "github.com/package-register/mocode/internal/agent/tools/builtin/exec"
	"github.com/package-register/mocode/internal/app"
	"github.com/package-register/mocode/internal/permission"
	"github.com/package-register/mocode/internal/pubsub"
	"github.com/package-register/mocode/internal/session"
	"github.com/package-register/mocode/internal/session/message"
	"github.com/package-register/mocode/internal/tools/shell"
	"github.com/package-register/mocode/internal/workspace"
)

func (s *Server) handleWebSocketStream(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("session_id")
	if sessionID == "" {
		http.Error(w, "missing session_id", http.StatusBadRequest)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Warn("Web: WS upgrade failed", "error", err)
		return
	}
	defer conn.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	slog.Info("Web: WS connected", "session", sessionID)

	appWS, ok := s.workspace.(*workspace.AppWorkspace)
	if !ok {
		http.Error(w, "web stream only supports local AppWorkspace for now", http.StatusNotImplemented)
		return
	}
	appInstance := appWS.App()
	writer := newWSConnWriter(conn)

	// Initial session status.
	writer.Send(map[string]any{
		"jsonrpc": "2.0",
		"method":  "session_status",
		"params": map[string]any{
			"session_id": sessionID,
			"state":      "idle",
			"seq":        0,
			"updated_at": now,
		},
	})

	// Replay persisted history.
	replayHistory(writer, sessionID)

	// Frontend expects top-level history_complete.
	writer.Send(map[string]any{
		"jsonrpc": "2.0",
		"method":  "history_complete",
		"id":      sessionID + "-history-complete",
	})

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	msgCh := appInstance.Messages.Subscribe(ctx)
	permReqCh := appInstance.Permissions.Subscribe(ctx)
	permNotifCh := appInstance.Permissions.SubscribeNotifications(ctx)
	agentNotifCh := appInstance.AgentNotifications().Subscribe(ctx)
	bridgeState := newWSBridgeState(appInstance, sessionID)
	resumeBackgroundToolStreams(ctx, writer, appInstance, sessionID)

	// Event bridge goroutine.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case ev, ok := <-msgCh:
				if !ok {
					return
				}
				if ev.Payload.SessionID != sessionID {
					continue
				}
				s.bridgeMessageEvent(ctx, writer, ev, bridgeState, appInstance)
			case ev, ok := <-permReqCh:
				if !ok {
					return
				}
				s.bridgePermissionRequest(writer, ev)
			case ev, ok := <-permNotifCh:
				if !ok {
					return
				}
				s.bridgePermissionNotification(writer, ev)
			case ev, ok := <-agentNotifCh:
				if !ok {
					return
				}
				if ev.Payload.SessionID != sessionID {
					continue
				}
				if ev.Payload.Type != notify.TypeSubagentCompleted || ev.Payload.SubagentCompleted == nil {
					continue
				}
				s.bridgeSubagentCompletedEvent(writer, *ev.Payload.SubagentCompleted)
			case <-ctx.Done():
				return
			}
		}
	}()

	// Read loop: prompt / cancel / set_plan_mode.
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			cancel()
			<-done
			slog.Debug("Web: WS closed", "session", sessionID, "error", err)
			break
		}

		var msg map[string]any
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		method, _ := msg["method"].(string)

		// Handle JSON-RPC responses (no method field, only id+result).
		// The frontend sends these for approval and question responses.
		if method == "" {
			if result, ok := msg["result"].(map[string]any); ok {
				if reqID, ok := result["request_id"].(string); ok {
					perm := permission.PermissionRequest{ID: reqID}
					// Approval response: {request_id, response: "approve"|"approve_for_session"|"reject"}
					if resp, ok := result["response"].(string); ok {
						switch resp {
						case "approve":
							s.workspace.PermissionGrant(perm)
						case "approve_for_session":
							s.workspace.PermissionGrantPersistent(perm)
						case "reject":
							s.workspace.PermissionDeny(perm)
						default:
							slog.Warn("Web: unknown approval response", "response", resp, "request_id", reqID)
						}
						writer.Send(map[string]any{
							"jsonrpc": "2.0",
							"id":      msg["id"],
							"result":  map[string]any{"status": "ok"},
						})
						continue
					}
					// Question response: {request_id, answers: {...}}
					if _, ok := result["answers"]; ok {
						// TODO: wire up question response mechanism when AskUserQuestion tool is implemented.
						writer.Send(map[string]any{
							"jsonrpc": "2.0",
							"id":      msg["id"],
							"result":  map[string]any{"status": "ok"},
						})
						continue
					}
				}
			}
		}

		switch method {
		case "initialize":
			writer.Send(map[string]any{
				"jsonrpc": "2.0",
				"id":      msg["id"],
				"result": map[string]any{
					"status":         "ok",
					"slash_commands": []any{},
				},
			})
			continue
		case "cancel":
			s.workspace.AgentCancel(sessionID)
			s.setRunning(sessionID, false)
			writer.Send(map[string]any{
				"jsonrpc": "2.0",
				"id":      msg["id"],
				"result":  map[string]any{},
			})
			// Notify frontend that the step was interrupted.
			writer.Send(map[string]any{
				"jsonrpc": "2.0",
				"method":  "event",
				"params": map[string]any{
					"type":    "StepInterrupted",
					"payload": map[string]any{},
				},
			})
			sendSessionStatus(writer, sessionID, "idle", "prompt_cancelled", "", bridgeState.NextStatusSeq())
			continue
		case "set_plan_mode":
			// Store plan mode state per session and broadcast via StatusUpdate.
			params, _ := msg["params"].(map[string]any)
			enabled := false
			if v, ok := params["enabled"].(bool); ok {
				enabled = v
			}
			s.setPlanMode(sessionID, enabled)

			// Notify frontend of plan mode status.
			writer.Send(map[string]any{
				"jsonrpc": "2.0",
				"method":  "event",
				"params": map[string]any{
					"type": "StatusUpdate",
					"payload": map[string]any{
						"plan_mode": enabled,
					},
				},
			})

			writer.Send(map[string]any{
				"jsonrpc": "2.0",
				"id":      msg["id"],
				"result": map[string]any{
					"status":         "ok",
					"slash_commands": []any{},
				},
			})
			continue
		case "prompt":
			// Explicitly handle prompt method.
		default:
			writer.Send(map[string]any{
				"jsonrpc": "2.0",
				"id":      msg["id"],
				"error": map[string]any{
					"code":    -32601,
					"message": "unknown method",
				},
			})
			continue
		}

		params, _ := msg["params"].(map[string]any)
		// Support both string and ContentPart array for user_input
		var userInput string
		if ui, ok := params["user_input"].(string); ok {
			userInput = ui
		} else if uiArr, ok := params["user_input"].([]any); ok {
			// Extract text from ContentPart array
			var parts []string
			for _, item := range uiArr {
				if part, ok := item.(map[string]any); ok {
					if text, ok := part["text"].(string); ok {
						parts = append(parts, text)
					}
				}
			}
			userInput = strings.Join(parts, "\n")
		}
		if userInput == "" {
			writer.Send(map[string]any{
				"jsonrpc": "2.0",
				"id":      msg["id"],
				"error": map[string]any{
					"code":    -32602,
					"message": "user_input must be a string in current implementation",
				},
			})
			continue
		}
		if s.isRunning(sessionID) {
			writer.Send(map[string]any{
				"jsonrpc": "2.0",
				"id":      msg["id"],
				"error": map[string]any{
					"code":    -32001,
					"message": "session is already running",
				},
			})
			continue
		}

		s.setRunning(sessionID, true)
		writer.Send(map[string]any{
			"jsonrpc": "2.0",
			"method":  "session_status",
			"params": map[string]any{
				"session_id": sessionID,
				"state":      "busy",
				"seq":        bridgeState.NextStatusSeq(),
				"updated_at": time.Now().UTC().Format(time.RFC3339),
			},
		})
		go func() {
			err := s.workspace.AgentRun(ctx, sessionID, userInput)
			if err != nil {
				if errors.Is(err, context.Canceled) || !s.isRunning(sessionID) {
					return
				}
				slog.Warn("Web: agent run failed", "session", sessionID, "error", err)
				s.setRunning(sessionID, false)
				sendSessionStatus(writer, sessionID, "error", "prompt_error", err.Error(), bridgeState.NextStatusSeq())
				return
			}
			if s.isRunning(sessionID) {
				s.setRunning(sessionID, false)
				sendSessionStatus(writer, sessionID, "idle", "prompt_complete", "", bridgeState.NextStatusSeq())
			}
		}()
		writer.Send(map[string]any{
			"jsonrpc": "2.0",
			"id":      msg["id"],
			"result": map[string]any{
				"status":         "ok",
				"slash_commands": getSlashCommands(),
			},
		})
	}
}

func (s *Server) bridgeMessageEvent(ctx context.Context, writer *wsConnWriter, ev pubsub.Event[message.Message], state *wsBridgeState, appInstance *app.App) {
	msg := ev.Payload

	switch ev.Type {
	case pubsub.CreatedEvent:
		if msg.Role == message.User {
			writer.Send(map[string]any{
				"jsonrpc": "2.0",
				"method":  "event",
				"params": map[string]any{
					"type": "TurnBegin",
					"payload": map[string]any{
						"user_input": msg.Content().Text,
					},
				},
			})
			return
		}
		if msg.Role == message.Assistant {
			writer.Send(map[string]any{
				"jsonrpc": "2.0",
				"method":  "event",
				"params": map[string]any{
					"type":    "StepBegin",
					"payload": map[string]any{"n": 1},
				},
			})
		}
	case pubsub.UpdatedEvent, pubsub.DeletedEvent:
		// Delta streaming below handles the current persisted message state.
	}

	if msg.Role == message.Assistant {
		for _, payload := range state.AssistantDeltaEvents(msg) {
			writer.Send(payload)
		}
		if msg.FinishPart() != nil {
			s.setRunning(msg.SessionID, false)
			sendSessionStatus(writer, msg.SessionID, "idle", "", "", state.NextStatusSeq())

			// Send StatusUpdate with token usage from session.
			if appInstance != nil {
				if sess, err := appInstance.Sessions.Get(context.Background(), msg.SessionID); err == nil {
					var contextUsage any
					maxCtx := int64(0)
					if cfg := appInstance.Config(); cfg != nil {
						// Use the same model lookup as handleGetConfig.
						if m, ok := cfg.Models["large"]; ok {
							if prov := cfg.GetModel(m.Provider, m.Model); prov != nil {
								maxCtx = prov.ContextWindow
							}
						}
					}
					if maxCtx > 0 {
						total := sess.PromptTokens + sess.CompletionTokens
						contextUsage = float64(total) / float64(maxCtx)
						if contextUsage.(float64) > 1 {
							contextUsage = 1.0
						}
					} else {
						contextUsage = nil
					}
					writer.Send(map[string]any{
						"jsonrpc": "2.0",
						"method":  "event",
						"params": map[string]any{
							"type": "StatusUpdate",
							"payload": map[string]any{
								"context_usage": contextUsage,
								"token_usage":   statusTokenUsage(sess),
							},
						},
					})
				}
			}
		}
		return
	}

	if msg.Role == message.Tool {
		for _, tr := range msg.ToolResults() {
			if !state.MarkToolResultSent(tr.ToolCallID) {
				continue
			}
			writer.Send(toolResultEvent(buildToolResultPayload(tr)))
			streamBackgroundToolResult(ctx, writer, tr)
		}
	}
}

func (s *Server) bridgePermissionRequest(writer *wsConnWriter, ev pubsub.Event[permission.PermissionRequest]) {
	p := ev.Payload
	writer.Send(map[string]any{
		"jsonrpc": "2.0",
		"method":  "event",
		"params": map[string]any{
			"type": "ApprovalRequest",
			"payload": map[string]any{
				"id":           p.ID,
				"action":       p.ToolName,
				"description":  p.Description,
				"sender":       "mocode",
				"tool_call_id": p.ToolCallID,
			},
		},
	})
}

func (s *Server) bridgePermissionNotification(writer *wsConnWriter, ev pubsub.Event[permission.PermissionNotification]) {
	n := ev.Payload
	response := any(false)
	if n.Granted && !n.Denied {
		response = true
	}
	writer.Send(map[string]any{
		"jsonrpc": "2.0",
		"method":  "event",
		"params": map[string]any{
			"type": "ApprovalRequestResolved",
			"payload": map[string]any{
				"request_id": n.RequestID,
				"response":   response,
			},
		},
	})
}

// bridgeQuestionRequest sends a QuestionRequest event to the frontend.
//
// TODO: When the AskUserQuestion tool is implemented, it should publish
// QuestionRequest events via pubsub. Subscribe to that topic and call
// this function to bridge the events to the WebSocket.
//
// Frontend expects payload: {id, tool_call_id, questions: [{question, header, options, multi_select}]}
//
//lint:ignore U1000 — reserved for future use.
func (s *Server) bridgeQuestionRequest(writer *wsConnWriter, reqID, toolCallID string, questions []any) {
	writer.Send(map[string]any{
		"jsonrpc": "2.0",
		"method":  "event",
		"params": map[string]any{
			"type": "QuestionRequest",
			"payload": map[string]any{
				"id":           reqID,
				"tool_call_id": toolCallID,
				"questions":    questions,
			},
		},
	})
}

// wsSender is the minimal surface the bridge helpers need to push a JSON-RPC
// payload to a connected client. *wsConnWriter implements it for production
// use; tests can substitute a recording implementation to assert on the
// produced payloads without standing up a real WebSocket.
type wsSender interface {
	Send(v any)
}

// bridgeSubagentEvent sends a SubagentEvent wrapping an inner event from a
// sub-agent (Agent tool) to the frontend.
//
// TODO: When the Agent tool produces sub-agent events via pubsub, subscribe
// to that topic and call this function to forward nested events.
//
// Frontend expects payload: {parent_tool_call_id, agent_id?, subagent_type?, event: {type, payload}}
//
//lint:ignore U1000 — reserved for future use.
func (s *Server) bridgeSubagentEvent(sender wsSender, parentToolCallID, agentID, subagentType string, innerType string, innerPayload any) {
	sender.Send(map[string]any{
		"jsonrpc": "2.0",
		"method":  "event",
		"params": map[string]any{
			"type": "SubagentEvent",
			"payload": map[string]any{
				"parent_tool_call_id": parentToolCallID,
				"agent_id":            agentID,
				"subagent_type":       subagentType,
				"event": map[string]any{
					"type":    innerType,
					"payload": innerPayload,
				},
			},
		},
	})
}

// bridgeSubagentCompletedEvent forwards a notify.SubagentCompletedEvent to the
// web frontend as a SubagentEvent whose inner type is "SubagentCompleted".
//
// The frontend already has a processSubagentEvent handler for arbitrary
// inner events; routing the completed notification through the same channel
// keeps the contract uniform and lets existing consumer logic drive state
// transitions on the parent tool call (e.g. subagentRunning -> false).
func (s *Server) bridgeSubagentCompletedEvent(sender wsSender, ev notify.SubagentCompletedEvent) {
	payload := map[string]any{
		"status":      string(ev.Status),
		"duration_ms": ev.DurationMs,
		//nolint:goconst // wire keys are intentionally stable for the JS client
		"usage": map[string]any{
			"input":          ev.Usage.Input,
			"output":         ev.Usage.Output,
			"cache_read":     ev.Usage.CacheRead,
			"cache_creation": ev.Usage.CacheCreation,
			"total":          ev.Usage.Total,
		},
	}
	if ev.Summary != "" {
		payload["summary"] = ev.Summary
	}
	if ev.Error != "" {
		payload["error"] = ev.Error
	}
	s.bridgeSubagentEvent(
		sender,
		ev.ParentToolCallID,
		ev.AgentID,
		ev.SubagentType,
		"SubagentCompleted",
		payload,
	)
}

func replayHistory(writer *wsConnWriter, sessionID string) {
	if !isSafeSessionPathComponent(sessionID) {
		return
	}

	workDir := getStartupDir()
	candidates := []string{
		filepath.Join(workDir, ".mocode", "sessions", sessionID, "wire.jsonl"),
	}

	for _, path := range candidates {
		data, err := os.ReadFile(path) //nolint:gosec // sessionID is validated by isSafeSessionPathComponent above.
		if err != nil {
			continue
		}

		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			var record struct {
				Message json.RawMessage `json:"message"`
				Type    string          `json:"type"`
			}
			if err := json.Unmarshal([]byte(line), &record); err != nil {
				continue
			}
			if record.Type == "metadata" {
				continue
			}

			writer.Send(map[string]any{
				"jsonrpc": "2.0",
				"method":  "event",
				"params":  record.Message,
			})
		}
		break
	}
}

func isSafeSessionPathComponent(value string) bool {
	if value == "" || value == "." || value == ".." {
		return false
	}

	if strings.Contains(value, "/") || strings.Contains(value, `\`) {
		return false
	}

	return filepath.Base(value) == value && filepath.Clean(value) == value
}

func sendSessionStatus(writer *wsConnWriter, sessionID, state, reason, detail string, seq int) {
	params := map[string]any{
		"session_id": sessionID,
		"state":      state,
		"seq":        seq,
		"updated_at": time.Now().UTC().Format(time.RFC3339),
	}
	if reason != "" {
		params["reason"] = reason
	}
	if detail != "" {
		params["detail"] = detail
	}
	writer.Send(map[string]any{
		"jsonrpc": "2.0",
		"method":  "session_status",
		"params":  params,
	})
}

func toolResultEvent(payload map[string]any) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"method":  "event",
		"params": map[string]any{
			"type":    "ToolResult",
			"payload": payload,
		},
	}
}

func buildToolResultPayload(tr message.ToolResult) map[string]any {
	return map[string]any{
		"tool_call_id": tr.ToolCallID,
		"return_value": buildToolReturnValue(tr),
	}
}

func buildToolReturnValue(tr message.ToolResult) map[string]any {
	returnValue := map[string]any{
		"is_error": tr.IsError,
		"output":   tr.Content,
		"message":  tr.Content,
		"display":  []any{},
	}
	if extras := bashToolResultExtras(tr); len(extras) > 0 {
		returnValue["extras"] = extras
	}
	return returnValue
}

func bashToolResultExtras(tr message.ToolResult) map[string]any {
	meta, ok := parseBashResponseMetadata(tr.Metadata)
	if !ok {
		return nil
	}
	status := "completed"
	if meta.Background && meta.ShellID != "" {
		status = "running"
	}
	return bashMetadataExtras(*meta, status)
}

func parseBashResponseMetadata(raw string) (*agentexec.BashResponseMetadata, bool) {
	if raw == "" {
		return nil, false
	}
	var meta agentexec.BashResponseMetadata
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		return nil, false
	}
	if meta.Description == "" && meta.WorkingDirectory == "" && meta.ShellID == "" && !meta.Background && !meta.TTY {
		return nil, false
	}
	return &meta, true
}

func resumeBackgroundToolStreams(ctx context.Context, writer *wsConnWriter, appInstance *app.App, sessionID string) {
	if appInstance == nil {
		return
	}
	msgs, err := appInstance.Messages.List(ctx, sessionID)
	if err != nil {
		return
	}
	for _, msg := range msgs {
		if msg.Role != message.Tool {
			continue
		}
		for _, tr := range msg.ToolResults() {
			streamBackgroundToolResult(ctx, writer, tr)
		}
	}
}

func streamBackgroundToolResult(ctx context.Context, writer *wsConnWriter, tr message.ToolResult) {
	meta, ok := parseBashResponseMetadata(tr.Metadata)
	if !ok || !meta.Background || meta.ShellID == "" {
		return
	}
	bgShell, ok := shell.GetBackgroundShellManager().Get(meta.ShellID)
	if !ok {
		return
	}
	go pollBackgroundToolResult(ctx, writer, tr.ToolCallID, *meta, bgShell)
}

func pollBackgroundToolResult(ctx context.Context, writer *wsConnWriter, toolCallID string, meta agentexec.BashResponseMetadata, bgShell *shell.BackgroundShell) {
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()

	var lastSignature string
	emit := func() bool {
		payload, signature, done := buildBackgroundToolResultPayload(toolCallID, meta, bgShell)
		if payload == nil {
			return true
		}
		if signature != lastSignature {
			lastSignature = signature
			writer.Send(toolResultEvent(payload))
		}
		return done
	}

	if emit() {
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if emit() {
				return
			}
		}
	}
}

func buildBackgroundToolResultPayload(toolCallID string, meta agentexec.BashResponseMetadata, bgShell *shell.BackgroundShell) (map[string]any, string, bool) {
	stdout, stderr, done, execErr := bgShell.GetOutput()
	status := backgroundShellStatus(meta.Background, done, execErr)
	output := formatBackgroundShellOutput(stdout, stderr, execErr)
	message := backgroundShellSummary(bgShell.ID, status)
	returnValue := map[string]any{
		"is_error": status == "error",
		"output":   output,
		"message":  message,
		"display":  []any{},
		"extras":   bashMetadataExtras(meta, status),
	}
	signature := strings.Join(
		[]string{
			status,
			output,
			message,
		},
		"\x00",
	)
	return map[string]any{
		"tool_call_id": toolCallID,
		"return_value": returnValue,
	}, signature, done
}

func bashMetadataExtras(meta agentexec.BashResponseMetadata, status string) map[string]any {
	extras := map[string]any{
		"background": meta.Background,
		"job_status": status,
	}
	if meta.ShellID != "" {
		extras["shell_id"] = meta.ShellID
	}
	if meta.Description != "" {
		extras["description"] = meta.Description
	}
	if meta.WorkingDirectory != "" {
		extras["working_directory"] = meta.WorkingDirectory
	}
	if meta.TTY {
		extras["tty"] = true
	}
	return extras
}

func backgroundShellStatus(background bool, done bool, execErr error) string {
	if !background {
		return "completed"
	}
	if !done {
		return "running"
	}
	if shell.IsInterrupt(execErr) {
		return "interrupted"
	}
	if execErr != nil {
		return "error"
	}
	return "completed"
}

func backgroundShellSummary(shellID, status string) string {
	switch status {
	case "running":
		return "Background task is still running."
	case "interrupted":
		return "Background task was interrupted."
	case "error":
		return "Background task failed."
	default:
		return "Background task completed."
	}
}

func formatBackgroundShellOutput(stdout, stderr string, execErr error) string {
	output := stdout
	if stderr != "" {
		if output != "" {
			output += "\n"
		}
		output += stderr
	}
	if execErr != nil && !shell.IsInterrupt(execErr) {
		if output != "" {
			output += "\n"
		}
		output += execErr.Error()
	}
	return truncateBackgroundShellOutput(output)
}

func truncateBackgroundShellOutput(content string) string {
	const maxOutputLength = 30000
	if len(content) <= maxOutputLength {
		return content
	}
	halfLength := maxOutputLength / 2
	start := content[:halfLength]
	end := content[len(content)-halfLength:]
	return start + "\n\n... output truncated ...\n\n" + end
}

type wsConnWriter struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func newWSConnWriter(conn *websocket.Conn) *wsConnWriter {
	return &wsConnWriter{conn: conn}
}

func (w *wsConnWriter) Send(v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	_ = w.conn.WriteMessage(websocket.TextMessage, data)
}

type wsBridgeState struct {
	statusSeq atomic.Int64

	messages        map[string]*wsBridgeMessageState
	sentToolResults map[string]struct{}
}

type wsBridgeMessageState struct {
	thinkingLen int
	textLen     int
	toolArgsLen map[string]int
	sentTools   map[string]struct{}
}

func newWSBridgeState(appInstance *app.App, sessionID string) *wsBridgeState {
	state := &wsBridgeState{
		messages:        make(map[string]*wsBridgeMessageState),
		sentToolResults: make(map[string]struct{}),
	}
	if appInstance == nil {
		return state
	}
	msgs, err := appInstance.Messages.List(context.Background(), sessionID)
	if err != nil {
		return state
	}
	for _, msg := range msgs {
		state.seedMessage(msg)
	}
	return state
}

func (s *wsBridgeState) NextStatusSeq() int {
	return int(s.statusSeq.Add(1))
}

func (s *wsBridgeState) MarkToolResultSent(toolCallID string) bool {
	if toolCallID == "" {
		return false
	}
	if _, ok := s.sentToolResults[toolCallID]; ok {
		return false
	}
	s.sentToolResults[toolCallID] = struct{}{}
	return true
}

func (s *wsBridgeState) AssistantDeltaEvents(msg message.Message) []map[string]any {
	msgState := s.messageState(msg.ID)
	events := make([]map[string]any, 0)

	thinking := msg.ReasoningContent().Thinking
	if len(thinking) < msgState.thinkingLen {
		msgState.thinkingLen = 0
	}
	if len(thinking) > msgState.thinkingLen {
		delta := thinking[msgState.thinkingLen:]
		msgState.thinkingLen = len(thinking)
		events = append(events, contentPartEvent("think", "think", delta))
	}

	text := msg.Content().Text
	if len(text) < msgState.textLen {
		msgState.textLen = 0
	}
	if len(text) > msgState.textLen {
		delta := text[msgState.textLen:]
		msgState.textLen = len(text)
		events = append(events, contentPartEvent("text", "text", delta))
	}

	for _, tc := range msg.ToolCalls() {
		if _, ok := msgState.sentTools[tc.ID]; !ok {
			msgState.sentTools[tc.ID] = struct{}{}
			msgState.toolArgsLen[tc.ID] = len(tc.Input)
			events = append(events, toolCallEvent(tc))
			continue
		}
		prevLen := msgState.toolArgsLen[tc.ID]
		if len(tc.Input) < prevLen {
			msgState.toolArgsLen[tc.ID] = len(tc.Input)
			continue
		}
		if len(tc.Input) > prevLen {
			delta := tc.Input[prevLen:]
			msgState.toolArgsLen[tc.ID] = len(tc.Input)
			events = append(events, toolCallPartEvent(delta))
		}
	}

	return events
}

func (s *wsBridgeState) seedMessage(msg message.Message) {
	switch msg.Role {
	case message.Assistant:
		msgState := s.messageState(msg.ID)
		msgState.thinkingLen = len(msg.ReasoningContent().Thinking)
		msgState.textLen = len(msg.Content().Text)
		for _, tc := range msg.ToolCalls() {
			msgState.sentTools[tc.ID] = struct{}{}
			msgState.toolArgsLen[tc.ID] = len(tc.Input)
		}
	case message.Tool:
		for _, tr := range msg.ToolResults() {
			if tr.ToolCallID == "" {
				continue
			}
			s.sentToolResults[tr.ToolCallID] = struct{}{}
		}
	case message.User, message.System:
		// Nothing to seed for websocket delta replay.
	}
}

func (s *wsBridgeState) messageState(messageID string) *wsBridgeMessageState {
	msgState, ok := s.messages[messageID]
	if ok {
		return msgState
	}
	msgState = &wsBridgeMessageState{
		toolArgsLen: make(map[string]int),
		sentTools:   make(map[string]struct{}),
	}
	s.messages[messageID] = msgState
	return msgState
}

func contentPartEvent(partType, field, value string) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"method":  "event",
		"params": map[string]any{
			"type": "ContentPart",
			"payload": map[string]any{
				"type": partType,
				field:  value,
			},
		},
	}
}

func toolCallEvent(tc message.ToolCall) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"method":  "event",
		"params": map[string]any{
			"type": "ToolCall",
			"payload": map[string]any{
				"type": "function",
				"id":   tc.ID,
				"function": map[string]any{
					"name":      tc.Name,
					"arguments": tc.Input,
				},
			},
		},
	}
}

func toolCallPartEvent(delta string) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"method":  "event",
		"params": map[string]any{
			"type": "ToolCallPart",
			"payload": map[string]any{
				"arguments_part": delta,
			},
		},
	}
}

func statusTokenUsage(sess session.Session) map[string]any {
	inputOther := sess.PromptTokens - sess.CacheReadTokens - sess.CacheCreationTokens
	if inputOther < 0 {
		inputOther = 0
	}
	return map[string]any{
		"input_other":          inputOther,
		"output":               sess.CompletionTokens,
		"input_cache_read":     sess.CacheReadTokens,
		"input_cache_creation": sess.CacheCreationTokens,
	}
}

// getSlashCommands returns the list of available slash commands for the frontend.
func getSlashCommands() []map[string]any {
	return []map[string]any{
		{"name": "help", "description": "Show help", "aliases": []string{"?"}},
		{"name": "clear", "description": "Clear the conversation", "aliases": []string{"reset"}},
		{"name": "compact", "description": "Compact/summarize the conversation", "aliases": []string{}},
	}
}
