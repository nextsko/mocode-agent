// Package workspace defines the Workspace interface used by all
// frontends (TUI, CLI) to interact with a running workspace. Two
// implementations exist: one wrapping a local app.App instance and one
// wrapping the HTTP client SDK.
package workspace

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/fantasy"
	"charm.land/catwalk/pkg/catwalk"

	"github.com/package-register/mocode/internal/core/agent/extension"
	"github.com/package-register/mocode/internal/core/agent/tools/lsp"
	mcptools "github.com/package-register/mocode/internal/core/agent/tools/mcp"
	"github.com/package-register/mocode/internal/core/config"
	"github.com/package-register/mocode/internal/core/permission"
	"github.com/package-register/mocode/internal/domain/history"
	"github.com/package-register/mocode/internal/domain/session"
	"github.com/package-register/mocode/internal/domain/session/message"
	"github.com/package-register/mocode/internal/ui/slash"
)

// LSPClientInfo holds information about an LSP client's state. This is
// the frontend-facing type; implementations translate from the
// underlying app or proto representation.
type LSPClientInfo struct {
	Name            string
	State           lsp.ServerState
	Error           error
	DiagnosticCount int
	ConnectedAt     time.Time
}

// LSPEventType represents the type of LSP event.
type LSPEventType string

const (
	LSPEventStateChanged       LSPEventType = "state_changed"
	LSPEventDiagnosticsChanged LSPEventType = "diagnostics_changed"
)

// LSPEvent represents an LSP event forwarded to the TUI.
type LSPEvent struct {
	Type            LSPEventType
	Name            string
	State           lsp.ServerState
	Error           error
	DiagnosticCount int
}

// AgentModel holds the model information exposed to the UI.
type AgentModel struct {
	AgentID    string
	Name       string
	CatwalkCfg catwalk.Model
	ModelCfg   config.SelectedModel
}

// AgentInfo holds summary information about an available agent for UI display.
type AgentInfo struct {
	ID          string
	Name        string
	Description string
	Model       string
	Provider    string
	SubAgents   []string
}

// Workspace is the main abstraction consumed by the TUI and CLI. It
// groups every operation a frontend needs to perform against a running
// workspace, regardless of whether the workspace is in-process or
// remote.
type Workspace interface {
	// Sessions
	CreateSession(ctx context.Context, title string) (session.Session, error)
	GetSession(ctx context.Context, sessionID string) (session.Session, error)
	ListSessions(ctx context.Context) ([]session.Session, error)
	SaveSession(ctx context.Context, sess session.Session) (session.Session, error)
	DeleteSession(ctx context.Context, sessionID string) error
	CreateAgentToolSessionID(messageID, toolCallID string) string
	ParseAgentToolSessionID(sessionID string) (messageID string, toolCallID string, ok bool)

	// Messages
	ListMessages(ctx context.Context, sessionID string) ([]message.Message, error)
	ListUserMessages(ctx context.Context, sessionID string) ([]message.Message, error)
	ListAllUserMessages(ctx context.Context) ([]message.Message, error)
	UpdateMessage(ctx context.Context, msg message.Message) error
	DeleteMessage(ctx context.Context, messageID string) error
	TruncateMessagesAfter(ctx context.Context, sessionID string, messageID string) (int, error)

	// Agent
	AgentRun(ctx context.Context, sessionID, prompt string, attachments ...message.Attachment) error
	AgentCancel(sessionID string)
	// AgentCancelSubagent stops a single sub-agent dispatched by the
	// Agent tool. subagentID is the user-visible identifier reported
	// on SubagentCompleted events (e.g. "<parentToolCallID>-1"). The
	// parent session is left running. No-op when the sub-agent is
	// unknown or already finished.
	AgentCancelSubagent(subagentID string)
	AgentIsBusy() bool
	AgentIsSessionBusy(sessionID string) bool
	AgentModel() AgentModel
	AgentIsReady() bool
	AgentQueuedPrompts(sessionID string) int
	AgentQueuedPromptsList(sessionID string) []string
	AgentClearQueue(sessionID string)
	AgentSummarize(ctx context.Context, sessionID string) error
	UpdateAgentModel(ctx context.Context) error
	InitCoderAgent(ctx context.Context) error
	GetDefaultSmallModel(providerID string) config.SelectedModel
	AvailableAgents() []AgentInfo
	CurrentAgentID() string
	SwitchAgent(ctx context.Context, agentID string) error
	// AgentSystemPrompt returns the proven system prompt of the active agent.
	// The /evo mode captures it at enter time as the stable base for
	// optimal-theory reconstruction ("maintain optimal theory").
	AgentSystemPrompt() string
	// AgentSmallLanguageModel returns the configured small model for cheap
	// auxiliary calls (the /evo lesson distiller), or nil if unconfigured.
	AgentSmallLanguageModel(ctx context.Context) fantasy.LanguageModel

	// Permissions
	PermissionGrant(perm permission.PermissionRequest)
	PermissionGrantPersistent(perm permission.PermissionRequest)
	PermissionDeny(perm permission.PermissionRequest)
	PermissionSkipRequests() bool
	PermissionSetSkipRequests(skip bool)

	// FileTracker
	FileTrackerRecordRead(ctx context.Context, sessionID, path string)
	FileTrackerLastReadTime(ctx context.Context, sessionID, path string) time.Time
	FileTrackerListReadFiles(ctx context.Context, sessionID string) ([]string, error)

	// History
	ListSessionHistory(ctx context.Context, sessionID string) ([]history.File, error)

	// LSP
	LSPStart(ctx context.Context, path string)
	LSPStopAll(ctx context.Context)
	LSPGetStates() map[string]LSPClientInfo
	LSPGetDiagnosticCounts(name string) lsp.DiagnosticCounts

	// Config (read-only data)
	Config() *config.Config
	WorkingDir() string
	Resolver() config.VariableResolver

	// Config mutations (proxied to server in client mode)
	UpdatePreferredModel(scope config.Scope, modelType config.SelectedModelType, model config.SelectedModel) error
	SetCompactMode(scope config.Scope, enabled bool) error
	SetProviderAPIKey(scope config.Scope, providerID string, apiKey any) error
	SetConfigField(scope config.Scope, key string, value any) error
	RemoveConfigField(scope config.Scope, key string) error
	RefreshOAuthToken(ctx context.Context, scope config.Scope, providerID string) error

	// Project lifecycle
	ProjectNeedsInitialization() (bool, error)
	MarkProjectInitialized() error
	InitializePrompt() (string, error)
	InitKnowledge(ctx context.Context) ([]string, error)

	// MCP operations (server-side in client mode)
	MCPGetStates() map[string]mcptools.ClientInfo
	MCPRefreshPrompts(ctx context.Context, name string)
	MCPRefreshResources(ctx context.Context, name string)
	RefreshMCPTools(ctx context.Context, name string)
	ReadMCPResource(ctx context.Context, name, uri string) ([]MCPResourceContents, error)
	GetMCPPrompt(clientID, promptID string, args map[string]string) (string, error)
	EnableMCP(ctx context.Context, name string) error
	DisableMCP(name string) error
	EnableDockerMCP(ctx context.Context) error
	DisableDockerMCP() error

	// Events
	Subscribe(program *tea.Program)
	Shutdown()

	// CommandRegistry returns a single unified registry built from all
	// registered providers. This is the single source of truth for both
	// the Slash Completions (input float layer) and Command Palette.
	BuildCommandRegistry() []slash.CommandDescriptor

	// AgentRegisterExtension attaches a lifecycle extension to the agent
	// coordinator (e.g. the /evo observability extension). The extension is
	// registered and enabled; dispatch considers it from the next run.
	AgentRegisterExtension(ext extension.Extension)
	// AgentUnregisterExtension detaches a lifecycle extension by name.
	AgentUnregisterExtension(name string)
}

// MCPResourceContents holds the contents of an MCP resource.
type MCPResourceContents struct {
	URI      string `json:"uri"`
	MIMEType string `json:"mime_type,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     []byte `json:"blob,omitempty"`
}
