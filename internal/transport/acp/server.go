package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// Runner binds protocol sessions to a concrete agent runtime.
type Runner interface {
	NewSession(ctx context.Context, cwd string) (SessionInfo, error)
	LoadSession(ctx context.Context, id, cwd string) (SessionInfo, bool, error)
	ResumeSession(ctx context.Context, id, cwd string) (SessionInfo, bool, error)
	ListSessions(ctx context.Context) ([]SessionInfo, error)
	DeleteSession(ctx context.Context, id string) error
	CloseSession(ctx context.Context, id string) error

	Prompt(ctx context.Context, sessionID, text string, emit func(SessionUpdate)) (stopReason string, err error)
	Cancel(sessionID string)

	Modes(ctx context.Context, sessionID string) (SessionModeState, error)
	SetMode(ctx context.Context, sessionID, modeID string) error
	ConfigOptions(ctx context.Context, sessionID string) ([]SessionConfigOption, error)
	SetConfigOption(ctx context.Context, sessionID, key, value string) ([]SessionConfigOption, error)
	AvailableCommands() []AvailableCommand
}

type sessionState struct {
	info   SessionInfo
	mode   string
	config []SessionConfigOption
}

// Server implements the agent side of ACP.
type Server struct {
	peer   *Peer
	runner Runner
	info   Implementation

	mu       sync.Mutex
	sessions map[string]*sessionState
	// permission, when set, asks the ACP client for approval.
	permission func(ctx context.Context, sessionID string, toolCall json.RawMessage, options []PermissionOption) (optionID string, cancelled bool)
}

// NewServer wires a peer and runner into an ACP agent.
func NewServer(peer *Peer, runner Runner, info Implementation) *Server {
	return &Server{peer: peer, runner: runner, info: info, sessions: map[string]*sessionState{}}
}

// SetPermissionRequester installs the client approval callback used by the
// runner when a tool needs permission.
func (s *Server) SetPermissionRequester(fn func(ctx context.Context, sessionID string, toolCall json.RawMessage, options []PermissionOption) (string, bool)) {
	s.permission = fn
}

// Handle dispatches one client→agent request.
func (s *Server) Handle(ctx context.Context, method string, params json.RawMessage) (any, *Error) {
	switch method {
	case "initialize":
		return s.initialize(params)
	case "authenticate":
		return AuthenticateResponse{}, nil
	case "logout":
		return LogoutResponse{}, nil
	case "session/new":
		return s.newSession(ctx, params)
	case "session/load":
		return s.loadSession(ctx, params)
	case "session/resume":
		return s.resumeSession(ctx, params)
	case "session/list":
		return s.listSessions(ctx)
	case "session/delete":
		return s.deleteSession(ctx, params)
	case "session/close":
		return s.closeSession(ctx, params)
	case "session/set_mode":
		return s.setMode(ctx, params)
	case "session/set_config_option":
		return s.setConfigOption(ctx, params)
	case "session/prompt":
		return s.prompt(ctx, params)
	default:
		return nil, &Error{Code: CodeMethodNotFound, Message: "method not found: " + method}
	}
}

// HandleNotification dispatches client→agent notifications.
func (s *Server) HandleNotification(method string, params json.RawMessage) {
	if method != "session/cancel" {
		return
	}
	var req CancelNotification
	if err := json.Unmarshal(params, &req); err != nil || req.SessionID == "" {
		return
	}
	s.runner.Cancel(req.SessionID)
}

func (s *Server) initialize(params json.RawMessage) (any, *Error) {
	var req InitializeRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, invalidParams(err)
	}
	version := ProtocolVersion
	if req.ProtocolVersion > 0 && req.ProtocolVersion < version {
		version = req.ProtocolVersion
	}
	return InitializeResponse{
		ProtocolVersion: version,
		AgentCapabilities: AgentCapabilities{
			LoadSession:        true,
			PromptCapabilities: PromptCapabilities{EmbeddedContext: true},
			McpCapabilities:    McpServerCapabilities(),
			SessionCapabilities: SessionCapabilities{
				List: &struct{}{}, Delete: &struct{}{}, Resume: &struct{}{}, Close: &struct{}{},
			},
		},
		AuthMethods: []AuthMethod{},
		AgentInfo:   s.info,
	}, nil
}

// McpServerCapabilities reports the MCP transports this build supports.
func McpServerCapabilities() McpCapabilities { return McpCapabilities{} }

func (s *Server) newSession(ctx context.Context, params json.RawMessage) (any, *Error) {
	var req NewSessionRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, invalidParams(err)
	}
	if !isAbs(req.Cwd) {
		return nil, invalidParams(fmt.Errorf("cwd must be an absolute path"))
	}
	info, err := s.runner.NewSession(ctx, req.Cwd)
	if err != nil {
		return nil, internal(err)
	}
	st := &sessionState{info: info}
	if modes, err := s.runner.Modes(ctx, info.SessionID); err == nil {
		st.mode = modes.CurrentModeID
	}
	if cfg, err := s.runner.ConfigOptions(ctx, info.SessionID); err == nil {
		st.config = cfg
	}
	s.mu.Lock()
	s.sessions[info.SessionID] = st
	s.mu.Unlock()
	s.emitCommands(info.SessionID)
	resp := NewSessionResponse{SessionID: info.SessionID, ConfigOptions: st.config}
	if st.mode != "" {
		modes, _ := s.runner.Modes(ctx, info.SessionID)
		resp.Modes = &modes
	}
	return resp, nil
}

func (s *Server) loadSession(ctx context.Context, params json.RawMessage) (any, *Error) {
	var req LoadSessionRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, invalidParams(err)
	}
	info, ok, err := s.runner.LoadSession(ctx, req.SessionID, req.Cwd)
	if err != nil {
		return nil, internal(err)
	}
	if !ok {
		return nil, &Error{Code: CodeInvalidParams, Message: "session not found: " + req.SessionID}
	}
	st := &sessionState{info: info}
	if cfg, err := s.runner.ConfigOptions(ctx, info.SessionID); err == nil {
		st.config = cfg
	}
	s.mu.Lock()
	s.sessions[info.SessionID] = st
	s.mu.Unlock()
	s.emitCommands(info.SessionID)
	return LoadSessionResponse{SessionID: info.SessionID, ConfigOptions: st.config}, nil
}

func (s *Server) resumeSession(ctx context.Context, params json.RawMessage) (any, *Error) {
	var req ResumeSessionRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, invalidParams(err)
	}
	info, ok, err := s.runner.ResumeSession(ctx, req.SessionID, req.Cwd)
	if err != nil {
		return nil, internal(err)
	}
	if !ok {
		return nil, &Error{Code: CodeInvalidParams, Message: "session not found: " + req.SessionID}
	}
	s.mu.Lock()
	st := &sessionState{info: info}
	s.sessions[info.SessionID] = st
	s.mu.Unlock()
	return ResumeSessionResponse{SessionID: info.SessionID}, nil
}

func (s *Server) listSessions(ctx context.Context) (any, *Error) {
	infos, err := s.runner.ListSessions(ctx)
	if err != nil {
		return nil, internal(err)
	}
	if infos == nil {
		infos = []SessionInfo{}
	}
	return ListSessionsResponse{Sessions: infos}, nil
}

func (s *Server) deleteSession(ctx context.Context, params json.RawMessage) (any, *Error) {
	var req DeleteSessionRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, invalidParams(err)
	}
	if err := s.runner.DeleteSession(ctx, req.SessionID); err != nil {
		return nil, internal(err)
	}
	s.mu.Lock()
	delete(s.sessions, req.SessionID)
	s.mu.Unlock()
	return DeleteSessionResponse{}, nil
}

func (s *Server) closeSession(ctx context.Context, params json.RawMessage) (any, *Error) {
	var req CloseSessionRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, invalidParams(err)
	}
	if err := s.runner.CloseSession(ctx, req.SessionID); err != nil {
		return nil, internal(err)
	}
	s.mu.Lock()
	delete(s.sessions, req.SessionID)
	s.mu.Unlock()
	return CloseSessionResponse{}, nil
}

func (s *Server) setMode(ctx context.Context, params json.RawMessage) (any, *Error) {
	var req SetSessionModeRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, invalidParams(err)
	}
	if err := s.runner.SetMode(ctx, req.SessionID, req.ModeID); err != nil {
		return nil, internal(err)
	}
	s.mu.Lock()
	if st, ok := s.sessions[req.SessionID]; ok {
		st.mode = req.ModeID
	}
	s.mu.Unlock()
	_ = s.peer.Notify("session/update", SessionNotification{
		SessionID: req.SessionID,
		Update:    SessionUpdate{SessionUpdate: "current_mode_update", CurrentModeID: req.ModeID},
	})
	return SetSessionModeResponse{}, nil
}

func (s *Server) setConfigOption(ctx context.Context, params json.RawMessage) (any, *Error) {
	var req SetSessionConfigOptionRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, invalidParams(err)
	}
	cfg, err := s.runner.SetConfigOption(ctx, req.SessionID, req.ConfigID, req.Value)
	if err != nil {
		return nil, internal(err)
	}
	s.mu.Lock()
	if st, ok := s.sessions[req.SessionID]; ok {
		st.config = cfg
	}
	s.mu.Unlock()
	_ = s.peer.Notify("session/update", SessionNotification{
		SessionID: req.SessionID,
		Update:    SessionUpdate{SessionUpdate: "config_option_update", ConfigOptions: cfg},
	})
	return SetSessionConfigOptionResponse{ConfigOptions: cfg}, nil
}

func (s *Server) prompt(ctx context.Context, params json.RawMessage) (any, *Error) {
	var req PromptRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, invalidParams(err)
	}
	text := promptText(req.Prompt)
	if strings.TrimSpace(text) == "" {
		return nil, invalidParams(fmt.Errorf("prompt must contain text"))
	}
	s.mu.Lock()
	_, known := s.sessions[req.SessionID]
	s.mu.Unlock()
	if !known {
		return nil, &Error{Code: CodeInvalidParams, Message: "unknown session: " + req.SessionID}
	}

	stop, err := s.runner.Prompt(ctx, req.SessionID, text, func(u SessionUpdate) {
		_ = s.peer.Notify("session/update", SessionNotification{SessionID: req.SessionID, Update: u})
	})
	if err != nil {
		if ctx.Err() != nil {
			return PromptResponse{StopReason: StopCancelled}, nil //nolint:nilerr // cancellation is a protocol outcome, not an error
		}
		return nil, internal(err)
	}
	return PromptResponse{StopReason: stop}, nil
}

func (s *Server) emitCommands(sessionID string) {
	cmds := s.runner.AvailableCommands()
	if len(cmds) == 0 {
		return
	}
	_ = s.peer.Notify("session/update", SessionNotification{
		SessionID: sessionID,
		Update:    SessionUpdate{SessionUpdate: "available_commands_update", AvailableCommands: cmds},
	})
}

// RequestPermission forwards a tool permission prompt to the client.
func (s *Server) RequestPermission(ctx context.Context, sessionID string, toolCall json.RawMessage) (string, bool, error) {
	if s.permission == nil {
		return "", true, fmt.Errorf("no permission handler wired")
	}
	id, cancelled := s.permission(ctx, sessionID, toolCall, permissionOptions())
	return id, cancelled, nil
}

// RequestPermissionSync is the callback shape the runner uses when a tool
// blocks on user approval.
func (s *Server) RequestPermissionSync(ctx context.Context, sessionID string, toolCall json.RawMessage) (string, bool) {
	if s.permission == nil {
		return "", true
	}
	return s.permission(ctx, sessionID, toolCall, permissionOptions())
}

func permissionOptions() []PermissionOption {
	return []PermissionOption{
		{OptionID: "allow_once", Name: "Allow once", Kind: "allow_once"},
		{OptionID: "allow_always", Name: "Always allow", Kind: "allow_always"},
		{OptionID: "reject_once", Name: "Reject", Kind: "reject_once"},
	}
}

// promptText flattens content blocks into a prompt string.
func promptText(blocks []ContentBlock) string {
	var b strings.Builder
	for _, blk := range blocks {
		switch blk.Type {
		case "text":
			b.WriteString(blk.Text)
			b.WriteString("\n")
		case "resource_link":
			fmt.Fprintf(&b, "[%s](%s)\n", firstNonEmpty(blk.Name, blk.URI), blk.URI)
		case "resource":
			if blk.Resource != nil {
				fmt.Fprintf(&b, "\n<resource uri=%q>\n%s\n</resource>\n", blk.Resource.URI, blk.Resource.Text)
			}
		}
	}
	return strings.TrimSpace(b.String())
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func invalidParams(err error) *Error {
	return &Error{Code: CodeInvalidParams, Message: err.Error()}
}

func internal(err error) *Error {
	return &Error{Code: CodeInternalError, Message: err.Error()}
}

func isAbs(p string) bool {
	return strings.HasPrefix(p, "/") || (len(p) > 2 && p[1] == ':' && (p[2] == '\\' || p[2] == '/'))
}
