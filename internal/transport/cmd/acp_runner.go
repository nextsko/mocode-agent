package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/nextsko/mocode-agent/internal/core/app"
	"github.com/nextsko/mocode-agent/internal/core/config"
	"github.com/nextsko/mocode-agent/internal/domain/session/message"
	"github.com/nextsko/mocode-agent/internal/transport/acp"
	"github.com/nextsko/mocode-agent/internal/transport/workspace"
)

// acpAppRunner implements acp.Runner on top of a mocode App workspace.
type acpAppRunner struct {
	ws  *workspace.AppWorkspace
	app *app.App

	mu     sync.Mutex
	permit func(ctx context.Context, sessionID string, toolCall json.RawMessage) (string, bool, error)
}

func newACPAppRunner(ws *workspace.AppWorkspace) *acpAppRunner {
	return &acpAppRunner{ws: ws, app: ws.App()}
}

func (r *acpAppRunner) setPermit(fn func(context.Context, string, json.RawMessage) (string, bool, error)) {
	r.permit = fn
}

func (r *acpAppRunner) NewSession(ctx context.Context, cwd string) (acp.SessionInfo, error) {
	// Resolve providers/credentials the same way the non-interactive runner
	// does; without this the coordinator can hold a model with no auth header.
	if err := r.ws.InitCoderAgent(ctx); err != nil {
		return acp.SessionInfo{}, err
	}
	if err := r.app.AgentCoordinator.UpdateModels(ctx); err != nil {
		return acp.SessionInfo{}, err
	}
	sess, err := r.ws.CreateSession(ctx, "ACP")
	if err != nil {
		return acp.SessionInfo{}, err
	}
	return acp.SessionInfo{SessionID: sess.ID, Cwd: cwd, Title: sess.Title}, nil
}

func (r *acpAppRunner) LoadSession(ctx context.Context, id, cwd string) (acp.SessionInfo, bool, error) {
	sess, err := r.ws.GetSession(ctx, id)
	if err != nil {
		return acp.SessionInfo{}, false, nil //nolint:nilerr // missing session maps to the protocol "not found" outcome
	}
	return acp.SessionInfo{SessionID: sess.ID, Cwd: cwd, Title: sess.Title}, true, nil
}

func (r *acpAppRunner) ResumeSession(ctx context.Context, id, cwd string) (acp.SessionInfo, bool, error) {
	return r.LoadSession(ctx, id, cwd)
}

func (r *acpAppRunner) ListSessions(ctx context.Context) ([]acp.SessionInfo, error) {
	sessions, err := r.ws.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]acp.SessionInfo, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, acp.SessionInfo{SessionID: s.ID, Title: s.Title, Cwd: r.ws.WorkingDir()})
	}
	return out, nil
}

func (r *acpAppRunner) DeleteSession(ctx context.Context, id string) error {
	return r.ws.DeleteSession(ctx, id)
}

func (r *acpAppRunner) CloseSession(_ context.Context, _ string) error { return nil }

func (r *acpAppRunner) Prompt(ctx context.Context, sessionID, text string, emit func(acp.SessionUpdate)) (string, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if r.permit != nil {
		go r.watchPermissions(ctx, sessionID)
	}

	msgs := r.app.Messages.Subscribe(ctx)
	done := make(chan error, 1)
	go func() {
		_, err := r.app.AgentCoordinator.Run(ctx, sessionID, text)
		done <- err
	}()

	textSeen := map[string]int{}
	reasonSeen := map[string]int{}
	toolSeen := map[string]bool{}
	for {
		select {
		case <-ctx.Done():
			r.app.AgentCoordinator.Cancel(sessionID)
			return acp.StopCancelled, nil
		case err := <-done:
			if err != nil {
				if ctx.Err() != nil {
					return acp.StopCancelled, nil
				}
				return "", err
			}
			return acp.StopEndTurn, nil
		case ev := <-msgs:
			msg := ev.Payload
			if msg.SessionID != sessionID || msg.Role != message.Assistant {
				continue
			}
			r.emitMessage(msg, emit, textSeen, reasonSeen, toolSeen)
		}
	}
}

// emitMessage turns one assistant message update into ACP chunks. Messages
// are re-published as they grow, so we diff by length per message id.
func (r *acpAppRunner) emitMessage(msg message.Message, emit func(acp.SessionUpdate), textSeen, reasonSeen map[string]int, toolSeen map[string]bool) {
	for _, part := range msg.Parts {
		switch p := part.(type) {
		case message.TextContent:
			prev := textSeen[msg.ID]
			if len(p.Text) > prev {
				emit(acp.SessionUpdate{
					SessionUpdate: "agent_message_chunk",
					MessageID:     msg.ID,
					Content:       &acp.ContentBlock{Type: "text", Text: p.Text[prev:]},
				})
				textSeen[msg.ID] = len(p.Text)
			}
		case message.ReasoningContent:
			prev := reasonSeen[msg.ID]
			if len(p.Thinking) > prev {
				emit(acp.SessionUpdate{
					SessionUpdate: "agent_thought_chunk",
					MessageID:     msg.ID,
					Content:       &acp.ContentBlock{Type: "text", Text: p.Thinking[prev:]},
				})
				reasonSeen[msg.ID] = len(p.Thinking)
			}
		case message.ToolCall:
			if !toolSeen[p.ID] {
				toolSeen[p.ID] = true
				raw, _ := json.Marshal(map[string]any{"input": p.Input})
				emit(acp.SessionUpdate{
					SessionUpdate: "tool_call",
					ToolCallID:    p.ID,
					Title:         p.Name,
					Name:          p.Name,
					Kind:          toolKind(p.Name),
					Status:        "pending",
					RawInput:      raw,
				})
			}
		case message.ToolResult:
			raw, _ := json.Marshal(map[string]any{"content": p.Content, "isError": p.IsError})
			status := "completed"
			if p.IsError {
				status = "failed"
			}
			emit(acp.SessionUpdate{
				SessionUpdate: "tool_call_update",
				ToolCallID:    p.ToolCallID,
				Name:          p.Name,
				Status:        status,
				RawOutput:     raw,
			})
		}
	}
}

func (r *acpAppRunner) Cancel(sessionID string) {
	if r.app.AgentCoordinator != nil {
		r.app.AgentCoordinator.Cancel(sessionID)
	}
}

func (r *acpAppRunner) Modes(ctx context.Context, _ string) (acp.SessionModeState, error) {
	agents := r.ws.AvailableAgents()
	modes := make([]acp.SessionMode, 0, len(agents))
	for _, a := range agents {
		modes = append(modes, acp.SessionMode{ID: a.ID, Name: a.Name, Description: a.Description})
	}
	cur := r.ws.CurrentAgentID()
	if cur == "" && len(modes) > 0 {
		cur = modes[0].ID
	}
	return acp.SessionModeState{CurrentModeID: cur, AvailableModes: modes}, nil
}

func (r *acpAppRunner) SetMode(ctx context.Context, _, modeID string) error {
	return r.ws.SwitchAgent(ctx, modeID)
}

func (r *acpAppRunner) ConfigOptions(_ context.Context, _ string) ([]acp.SessionConfigOption, error) {
	cfg := r.ws.Config()
	if cfg == nil {
		return nil, nil
	}
	cur := cfg.Models[config.SelectedModelTypeLarge]
	opts := make([]acp.ConfigSelectOption, 0, 8)
	for _, id := range cfg.View().ModelIDs() {
		opts = append(opts, acp.ConfigSelectOption{Value: id, Name: id})
	}
	return []acp.SessionConfigOption{{
		ID:           "model",
		Name:         "Model",
		Description:  "Large model used for this session",
		Type:         "select",
		CurrentValue: cur.Provider + "/" + cur.Model,
		Options:      opts,
	}}, nil
}

func (r *acpAppRunner) SetConfigOption(ctx context.Context, _, key, value string) ([]acp.SessionConfigOption, error) {
	if key != "model" {
		return nil, fmt.Errorf("unknown config option %q", key)
	}
	provider, model, ok := strings.Cut(value, "/")
	if !ok {
		return nil, fmt.Errorf("model must be provider/model")
	}
	if err := r.ws.UpdatePreferredModel(config.ScopeGlobal, config.SelectedModelTypeLarge, config.SelectedModel{Provider: provider, Model: model}); err != nil {
		return nil, err
	}
	if err := r.ws.App().UpdateAgentModel(ctx); err != nil {
		return nil, err
	}
	return r.ConfigOptions(ctx, "")
}

func (r *acpAppRunner) AvailableCommands() []acp.AvailableCommand {
	descriptors := r.ws.BuildCommandRegistry()
	out := make([]acp.AvailableCommand, 0, len(descriptors))
	for _, d := range descriptors {
		out = append(out, acp.AvailableCommand{Name: strings.TrimPrefix(firstNonEmptyStr(d.Shortcut, d.ID), "/"), Description: d.Description})
	}
	return out
}

// watchPermissions bridges mocode permission requests to ACP prompts.
func (r *acpAppRunner) watchPermissions(ctx context.Context, sessionID string) {
	events := r.app.Permissions.Subscribe(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-events:
			req := ev.Payload
			if req.SessionID != sessionID {
				continue
			}
			raw, _ := json.Marshal(map[string]any{
				"toolCallId": req.ToolCallID,
				"title":      firstNonEmptyStr(req.Description, req.ToolName, req.Action),
				"name":       req.ToolName,
				"kind":       toolKind(req.ToolName),
				"status":     "pending",
			})
			option, cancelled, err := r.permit(ctx, sessionID, raw)
			if err != nil || cancelled {
				r.app.Permissions.Deny(req)
				continue
			}
			switch option {
			case "allow_always":
				r.app.Permissions.GrantPersistent(req)
			case "allow_once":
				r.app.Permissions.Grant(req)
			default:
				r.app.Permissions.Deny(req)
			}
		}
	}
}

func toolKind(name string) string {
	switch strings.ToLower(name) {
	case "bash", "shell":
		return "execute"
	case "edit", "write", "multiedit":
		return "edit"
	case "read", "view", "glob", "grep":
		return "read"
	case "webfetch", "websearch":
		return "fetch"
	default:
		return "other"
	}
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
