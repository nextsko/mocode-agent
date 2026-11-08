package acp

import "encoding/json"

// ProtocolVersion is the ACP version implemented here.
const ProtocolVersion = 1

// Implementation identifies the agent or client.
type Implementation struct {
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version"`
}

// ContentBlock is one prompt/content item. Type selects which fields apply.
type ContentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	URI      string `json:"uri,omitempty"`
	Name     string `json:"name,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Data     string `json:"data,omitempty"`
	// EmbeddedResource fields
	Resource *EmbeddedResource `json:"resource,omitempty"`
}

// EmbeddedResource is inline context supplied by the client.
type EmbeddedResource struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     string `json:"blob,omitempty"`
}

// Annotations attach audience/last-modified hints to content.
type Annotations struct {
	Audience     []string `json:"audience,omitempty"`
	LastModified string   `json:"lastModified,omitempty"`
	Priority     *float64 `json:"priority,omitempty"`
}

// PromptCapabilities describes optional prompt content support.
type PromptCapabilities struct {
	Image           bool `json:"image"`
	Audio           bool `json:"audio"`
	EmbeddedContext bool `json:"embeddedContext"`
}

// McpCapabilities describes MCP transports supported by the agent.
type McpCapabilities struct {
	HTTP bool `json:"http"`
	SSE  bool `json:"sse"`
}

// SessionCapabilities describes optional session lifecycle methods.
type SessionCapabilities struct {
	List   *struct{} `json:"list,omitempty"`
	Delete *struct{} `json:"delete,omitempty"`
	Resume *struct{} `json:"resume,omitempty"`
	Close  *struct{} `json:"close,omitempty"`
}

// AgentCapabilities advertises what this agent supports.
type AgentCapabilities struct {
	LoadSession         bool                `json:"loadSession"`
	PromptCapabilities  PromptCapabilities  `json:"promptCapabilities"`
	McpCapabilities     McpCapabilities     `json:"mcpCapabilities"`
	SessionCapabilities SessionCapabilities `json:"sessionCapabilities"`
}

// InitializeRequest is the first client request.
type InitializeRequest struct {
	ProtocolVersion    int                `json:"protocolVersion"`
	ClientCapabilities ClientCapabilities `json:"clientCapabilities"`
	ClientInfo         *Implementation    `json:"clientInfo,omitempty"`
}

// ClientCapabilities mirrors the client's supported features.
type ClientCapabilities struct {
	Fs struct {
		ReadTextFile  bool `json:"readTextFile"`
		WriteTextFile bool `json:"writeTextFile"`
	} `json:"fs"`
	Terminal bool `json:"terminal"`
}

// InitializeResponse is the negotiation result.
type InitializeResponse struct {
	ProtocolVersion   int               `json:"protocolVersion"`
	AgentCapabilities AgentCapabilities `json:"agentCapabilities"`
	AuthMethods       []AuthMethod      `json:"authMethods"`
	AgentInfo         Implementation    `json:"agentInfo"`
}

// AuthMethod is one advertised authentication method.
type AuthMethod struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// NewSessionRequest creates a session rooted at cwd.
type NewSessionRequest struct {
	Cwd            string      `json:"cwd"`
	McpServers     []McpServer `json:"mcpServers"`
	AdditionalDirs []string    `json:"additionalDirectories,omitempty"`
}

// McpServer is an MCP server the client wants the agent to connect.
type McpServer struct {
	Name    string   `json:"name"`
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
	Env     []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"env,omitempty"`
	URL string `json:"url,omitempty"`
}

// SessionMode is one agent mode.
type SessionMode struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// SessionModeState reports available/current modes.
type SessionModeState struct {
	CurrentModeID  string        `json:"currentModeId"`
	AvailableModes []SessionMode `json:"availableModes"`
}

// ConfigSelectOption is one value of a select config option.
type ConfigSelectOption struct {
	Value       string `json:"value"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// SessionConfigOption is a session-scoped setting such as the model.
type SessionConfigOption struct {
	ID           string               `json:"id"`
	Name         string               `json:"name"`
	Description  string               `json:"description,omitempty"`
	CurrentValue string               `json:"currentValue"`
	Options      []ConfigSelectOption `json:"options,omitempty"`
	Type         string               `json:"type,omitempty"`
}

// NewSessionResponse returns the new session's id and initial state.
type NewSessionResponse struct {
	SessionID     string                `json:"sessionId"`
	Modes         *SessionModeState     `json:"modes,omitempty"`
	ConfigOptions []SessionConfigOption `json:"configOptions,omitempty"`
}

// LoadSessionRequest resumes an existing session.
type LoadSessionRequest struct {
	SessionID      string      `json:"sessionId"`
	Cwd            string      `json:"cwd"`
	McpServers     []McpServer `json:"mcpServers"`
	AdditionalDirs []string    `json:"additionalDirectories,omitempty"`
}

// LoadSessionResponse mirrors NewSessionResponse.
type LoadSessionResponse = NewSessionResponse

// SessionInfo is one entry from session/list.
type SessionInfo struct {
	SessionID string `json:"sessionId"`
	Cwd       string `json:"cwd"`
	Title     string `json:"title,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

// ListSessionsResponse lists known sessions.
type ListSessionsResponse struct {
	Sessions []SessionInfo `json:"sessions"`
}

// ResumeSessionRequest reattaches to a session.
type ResumeSessionRequest struct {
	SessionID  string      `json:"sessionId"`
	Cwd        string      `json:"cwd"`
	McpServers []McpServer `json:"mcpServers"`
}

// ResumeSessionResponse mirrors NewSessionResponse.
type ResumeSessionResponse = NewSessionResponse

// DeleteSessionRequest removes a session.
type DeleteSessionRequest struct {
	SessionID string `json:"sessionId"`
}

// DeleteSessionResponse is empty.
type DeleteSessionResponse struct{}

// CloseSessionRequest closes a session's resources.
type CloseSessionRequest struct {
	SessionID string `json:"sessionId"`
}

// CloseSessionResponse is empty.
type CloseSessionResponse struct{}

// PromptRequest carries content blocks for one turn.
type PromptRequest struct {
	SessionID string         `json:"sessionId"`
	Prompt    []ContentBlock `json:"prompt"`
}

// PromptResponse reports why the turn ended.
type PromptResponse struct {
	StopReason string `json:"stopReason"`
}

// Stop reasons.
const (
	StopEndTurn   = "end_turn"
	StopMaxTokens = "max_tokens"
	StopMaxTurn   = "max_turn_requests"
	StopRefusal   = "refusal"
	StopCancelled = "cancelled"
)

// SessionNotification carries one streaming update.
type SessionNotification struct {
	SessionID string        `json:"sessionId"`
	Update    SessionUpdate `json:"update"`
}

// SessionUpdate is the union of streamed update kinds.
type SessionUpdate struct {
	SessionUpdate string `json:"sessionUpdate"`

	// content chunks
	Content   *ContentBlock `json:"content,omitempty"`
	MessageID string        `json:"messageId,omitempty"`

	// tool calls
	ToolCallID string          `json:"toolCallId,omitempty"`
	Title      string          `json:"title,omitempty"`
	Kind       string          `json:"kind,omitempty"`
	Status     string          `json:"status,omitempty"`
	Name       string          `json:"name,omitempty"`
	RawInput   json.RawMessage `json:"rawInput,omitempty"`
	RawOutput  json.RawMessage `json:"rawOutput,omitempty"`

	// plan
	Entries []PlanEntry `json:"entries,omitempty"`

	// commands / modes / config
	AvailableCommands []AvailableCommand    `json:"availableCommands,omitempty"`
	CurrentModeID     string                `json:"currentModeId,omitempty"`
	ConfigOptions     []SessionConfigOption `json:"configOptions,omitempty"`
}

// PlanEntry is one step of a reported plan.
type PlanEntry struct {
	Content  string `json:"content"`
	Priority string `json:"priority,omitempty"`
	Status   string `json:"status,omitempty"`
}

// AvailableCommand is a slash command the agent offers.
type AvailableCommand struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// PermissionOption is one choice in a permission prompt.
type PermissionOption struct {
	OptionID string `json:"optionId"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
}

// RequestPermissionRequest asks the user to approve a tool call.
type RequestPermissionRequest struct {
	SessionID string             `json:"sessionId"`
	ToolCall  json.RawMessage    `json:"toolCall"`
	Options   []PermissionOption `json:"options"`
}

// RequestPermissionResponse selects or cancels.
type RequestPermissionResponse struct {
	Outcome RequestPermissionOutcome `json:"outcome"`
}

// RequestPermissionOutcome is "selected" with optionId, or "cancelled".
type RequestPermissionOutcome struct {
	Outcome  string `json:"outcome"`
	OptionID string `json:"optionId,omitempty"`
}

// ReadTextFileRequest asks the client for file content.
type ReadTextFileRequest struct {
	SessionID string `json:"sessionId"`
	Path      string `json:"path"`
	Line      *int   `json:"line,omitempty"`
	Limit     *int   `json:"limit,omitempty"`
}

// ReadTextFileResponse returns the content.
type ReadTextFileResponse struct {
	Content string `json:"content"`
}

// WriteTextFileRequest asks the client to write a file.
type WriteTextFileRequest struct {
	SessionID string `json:"sessionId"`
	Path      string `json:"path"`
	Content   string `json:"content"`
}

// WriteTextFileResponse is empty.
type WriteTextFileResponse struct{}

// CreateTerminalRequest asks the client to run a command.
type CreateTerminalRequest struct {
	SessionID string   `json:"sessionId"`
	Command   string   `json:"command"`
	Args      []string `json:"args,omitempty"`
	Cwd       string   `json:"cwd,omitempty"`
}

// CreateTerminalResponse returns the terminal id.
type CreateTerminalResponse struct {
	TerminalID string `json:"terminalId"`
}

// TerminalOutputRequest asks for buffered output.
type TerminalOutputRequest struct {
	SessionID  string `json:"sessionId"`
	TerminalID string `json:"terminalId"`
}

// TerminalOutputResponse returns output and exit status.
type TerminalOutputResponse struct {
	Output     string `json:"output"`
	Truncated  bool   `json:"truncated"`
	ExitStatus *struct {
		ExitCode *int   `json:"exitCode,omitempty"`
		Signal   string `json:"signal,omitempty"`
	} `json:"exitStatus,omitempty"`
}

// WaitForTerminalExitRequest waits for a terminal to finish.
type WaitForTerminalExitRequest struct {
	SessionID  string `json:"sessionId"`
	TerminalID string `json:"terminalId"`
}

// WaitForTerminalExitResponse returns the exit status.
type WaitForTerminalExitResponse struct {
	ExitCode *int   `json:"exitCode,omitempty"`
	Signal   string `json:"signal,omitempty"`
}

// KillTerminalRequest asks the client to kill a terminal.
type KillTerminalRequest struct {
	SessionID  string `json:"sessionId"`
	TerminalID string `json:"terminalId"`
}

// KillTerminalResponse is empty.
type KillTerminalResponse struct{}

// ReleaseTerminalRequest asks the client to free a terminal.
type ReleaseTerminalRequest struct {
	SessionID  string `json:"sessionId"`
	TerminalID string `json:"terminalId"`
}

// ReleaseTerminalResponse is empty.
type ReleaseTerminalResponse struct{}

// SetSessionModeRequest switches the session mode.
type SetSessionModeRequest struct {
	SessionID string `json:"sessionId"`
	ModeID    string `json:"modeId"`
}

// SetSessionModeResponse is empty.
type SetSessionModeResponse struct{}

// SetSessionConfigOptionRequest changes one option.
type SetSessionConfigOptionRequest struct {
	SessionID string `json:"sessionId"`
	ConfigID  string `json:"configId"`
	Value     string `json:"value"`
}

// SetSessionConfigOptionResponse returns the updated options.
type SetSessionConfigOptionResponse struct {
	ConfigOptions []SessionConfigOption `json:"configOptions"`
}

// CancelNotification tells the agent to stop the current turn.
type CancelNotification struct {
	SessionID string `json:"sessionId"`
}

// AuthenticateRequest performs an auth method.
type AuthenticateRequest struct {
	MethodID string `json:"methodId"`
}

// AuthenticateResponse is empty.
type AuthenticateResponse struct{}

// LogoutRequest clears auth state.
type LogoutRequest struct{}

// LogoutResponse is empty.
type LogoutResponse struct{}

// ReplayMessage is one conversation entry replayed on session/load.
type ReplayMessage struct {
	Role      string `json:"role"` // user | assistant
	MessageID string `json:"messageId,omitempty"`
	Text      string `json:"text"`
}
