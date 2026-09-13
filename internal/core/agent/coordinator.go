package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"charm.land/fantasy"
	"github.com/nextsko/mocode-agent/internal/core/agent/jobs"
	"github.com/nextsko/mocode-agent/internal/core/agent/notify"
	"github.com/nextsko/mocode-agent/internal/core/agent/prompt"
	"github.com/nextsko/mocode-agent/internal/core/agent/summary"
	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/core/config"
	"github.com/nextsko/mocode-agent/internal/core/permission"
	"github.com/nextsko/mocode-agent/internal/core/question"
	"github.com/nextsko/mocode-agent/internal/core/shellruntime/shell"
	"github.com/nextsko/mocode-agent/internal/core/skills"
	"github.com/nextsko/mocode-agent/internal/core/tools"
	"github.com/nextsko/mocode-agent/internal/core/tools/external/nethttp"
	"github.com/nextsko/mocode-agent/internal/core/tools/internalx/lsp"
	"github.com/nextsko/mocode-agent/internal/domain/filetracker"
	"github.com/nextsko/mocode-agent/internal/domain/history"
	"github.com/nextsko/mocode-agent/internal/domain/messenger"
	"github.com/nextsko/mocode-agent/internal/domain/session"
	"github.com/nextsko/mocode-agent/internal/domain/session/message"
	"github.com/nextsko/mocode-agent/internal/domain/session/sessionlog"
	"github.com/nextsko/mocode-agent/internal/store"
	"github.com/nextsko/mocode-agent/internal/util/csync"
	"github.com/nextsko/mocode-agent/internal/util/errcoll"
	"github.com/nextsko/mocode-agent/internal/util/infra"
	"github.com/nextsko/mocode-agent/internal/util/pubsub"
	"golang.org/x/sync/errgroup"
)

// Coordinator errors.
var (
	errDefaultAgentNotConfigured       = errors.New("default agent not configured")
	errModelProviderNotConfigured      = errors.New("model provider not configured")
	errLargeModelNotSelected           = errors.New("large model not selected")
	errSmallModelNotSelected           = errors.New("small model not selected")
	errLargeModelProviderNotConfigured = errors.New("large model provider not configured")
	errSmallModelProviderNotConfigured = errors.New("small model provider not configured")
	errLargeModelNotFound              = errors.New("large model not found in provider config")
	errSmallModelNotFound              = errors.New("small model not found in provider config")
)

// SummaryCompletedMsg is the payload published on the coordinator's
// summaryDone broker whenever an asynchronous session summary finishes
// (success or failure). The TUI subscribes via app.events and renders
// a completion InfoMsg / ErrorMsg. Path is empty on failure; Err is
// nil on success.
type SummaryCompletedMsg struct {
	SessionID string
	Path      string
	Err       error
}

type Coordinator interface {
	// SetMainAgent switches the active agent to the given mode/agent ID.
	// This supports transfer_to_agent and mode switching.
	SetMainAgent(agentID string) error
	// SetMessenger wires the external-account send port (e.g. messaging
	// integration) that the coordinator-owned messaging tools use. Safe to
	// call once at startup.
	SetMessenger(m messenger.Messenger)
	Run(ctx context.Context, sessionID, prompt string, attachments ...message.Attachment) (*fantasy.AgentResult, error)
	Cancel(sessionID string)
	// InjectGuidance adds mid-turn user guidance to the RUNNING turn of the
	// session: persisted as a user message and surfaced to the model at its
	// next step (steering without a new queued turn).
	InjectGuidance(ctx context.Context, sessionID, text string) error
	// ForceRun preempts the session: the prompt jumps the queue head and the
	// running turn is interrupted so dispatch continues into it immediately.
	ForceRun(ctx context.Context, sessionID, prompt string, attachments ...message.Attachment) error
	// CancelSubagent stops a single sub-agent dispatched by the Agent
	// tool without cancelling the parent session. subagentID is the
	// user-visible identifier (params.AgentID), e.g. "<parentToolCallID>-1".
	// No-op when the sub-agent is unknown or already finished.
	CancelSubagent(subagentID string)
	CancelAll()
	IsSessionBusy(sessionID string) bool
	IsBusy() bool
	QueuedPrompts(sessionID string) int
	QueuedPromptsList(sessionID string) []string
	ClearQueue(sessionID string)
	Summarize(context.Context, string) error
	// EnqueueSummaryAndDrain enqueues a session for asynchronous summary
	// generation and immediately drains the queue. Unlike Summarize, it
	// never blocks the caller; the LLM-driven generation runs in a
	// goroutine on context.Background(). Used by the /summary slash
	// command path to keep the TUI interactive while the summary runs.
	EnqueueSummaryAndDrain(sessionID string)
	// SummarySubscribe exposes the channel of SummaryCompletedMsg events
	// emitted by the asynchronous summary goroutine. The composition root
	// (internal/core/app/app.go) forwards these into app.events so the
	// TUI can render a completion InfoMsg without polling.
	SummarySubscribe(ctx context.Context) <-chan pubsub.Event[SummaryCompletedMsg]
	Model() Model
	ActiveAgentID() string
	// ActiveAgentSystemPrompt returns the proven system prompt of the active
	// agent. The /evo mode captures it at enter time so the reconstructed
	// optimal theory preserves what already worked as a stable base.
	ActiveAgentSystemPrompt() string
	// SmallLanguageModel returns the configured small model, used for cheap
	// auxiliary calls (the /evo lesson distiller). Returns nil when no small
	// model is configured, in which case the distiller falls back to its
	// zero-overhead default.
	SmallLanguageModel(ctx context.Context) fantasy.LanguageModel
	UpdateModels(ctx context.Context) error
	// Close releases coordinator-owned resources (the tool registry's
	// connection pools, e.g. SSH). Called from the composition root's
	// Shutdown after all agents are cancelled.
	Close(ctx context.Context) error
}

type coordinator struct {
	cfg         *config.ConfigStore
	sessions    session.Service
	messages    message.Service
	permissions permission.Service
	questions   question.Service
	history     history.Service
	filetracker filetracker.Service
	lspManager  *lsp.Manager
	notify      pubsub.Publisher[notify.Notification]

	currentAgent  SessionAgent
	activeAgentID string
	agents        map[string]SessionAgent
	// toolRegistry is the ONE registry for the coordinator's lifetime.
	// Previously every buildTools call (mode switch, UpdateModels, sub-agent)
	// built a fresh registry, leaking a new SSH connection pool each time
	// because nothing ever called StopAll. Close() now drains it.
	toolRegistry *tools.Registry
	// httpFactory is the shared outbound-HTTP port for every tool build.
	httpFactory *nethttp.Factory
	// subagentIndex maps the user-visible sub-agent ID (params.AgentID,
	// e.g. "<parentToolCallID>-1") to the internal sub-session ID used by
	// sessionAgent.Cancel. The mapping is populated by runSubAgentWithMeta
	// before the sub-agent goroutine starts and removed in a defer so
	// that cancelled sub-agents can be stopped without affecting the
	// parent session.
	subagentIndex *csync.Map[string, string]
	summaryQueue  *summary.Queue
	// summaryDone emits a SummaryCompletedMsg whenever the asynchronous
	// summary goroutine finishes (success or failure). The composition
	// root (internal/core/app/app.go) subscribes to it inside
	// InitCoderAgent and forwards events into app.events so the TUI can
	// render the completion state via Update().
	summaryDone    *pubsub.Broker[SummaryCompletedMsg]
	sessionLogDir  string // base dir for session logs
	errorCollector *errcoll.Collector
	sessionSearch  *store.SessionSearch
	// messenger is the external-account send port (e.g. messaging
	// integration). Defaults to NoopMessenger; wired from the composition
	// root after construction.
	messenger messenger.Messenger
	// Skills discovery results (session-start snapshot).
	allSkills    []*skills.Skill // Pre-filter: all discovered after dedup.
	activeSkills []*skills.Skill // Post-filter: active skills only.
	skillTracker *skills.Tracker

	readyWg errgroup.Group
}

func NewCoordinator(
	ctx context.Context,
	cfg *config.ConfigStore,
	sessions session.Service,
	messages message.Service,
	permissions permission.Service,
	questions question.Service,
	history history.Service,
	filetracker filetracker.Service,
	lspManager *lsp.Manager,
	notify pubsub.Publisher[notify.Notification],
	errorCollector *errcoll.Collector,
	sessionSearch *store.SessionSearch,
) (Coordinator, error) {
	// Discover skills once at session start.
	allSkills, activeSkills := discoverSkills(cfg)
	skillTracker := skills.NewTracker(activeSkills)

	c := &coordinator{
		cfg:            cfg,
		sessions:       sessions,
		messages:       messages,
		permissions:    permissions,
		questions:      questions,
		history:        history,
		filetracker:    filetracker,
		lspManager:     lspManager,
		notify:         notify,
		agents:         make(map[string]SessionAgent),
		subagentIndex:  csync.NewMap[string, string](),
		summaryQueue:   summary.NewQueue(),
		summaryDone:    pubsub.NewBroker[SummaryCompletedMsg](),
		allSkills:      allSkills,
		activeSkills:   activeSkills,
		skillTracker:   skillTracker,
		sessionLogDir:  filepath.Join(infra.DataDir(), "session-logs"),
		errorCollector: errorCollector,
		sessionSearch:  sessionSearch,
		toolRegistry:   tools.NewRegistry(),
		httpFactory:    tools.NewHTTPFactory(cfg),
	}

	// Resolve the active agent from config (supports mode switching).
	activeAgentID := config.AgentDefault
	if cfg.Config().Options != nil && cfg.Config().Options.ActiveMode != "" {
		activeAgentID = cfg.Config().Options.ActiveMode
	}

	agentCfg, ok := cfg.Config().Agents[activeAgentID]
	if !ok {
		// Fallback to default agent if the active mode is missing.
		agentCfg, ok = cfg.Config().Agents[config.AgentDefault]
		if !ok {
			return nil, errDefaultAgentNotConfigured
		}
		activeAgentID = config.AgentDefault
	}

	promptOpts := []prompt.Option{prompt.WithWorkingDir(c.cfg.WorkingDir())}
	agentPrompt, err := prompt.PromptForAgent(agentCfg, promptOpts...)
	if err != nil {
		return nil, err
	}

	agent, err := c.buildAgent(ctx, agentPrompt, agentCfg, false)
	if err != nil {
		return nil, err
	}
	c.currentAgent = agent
	c.activeAgentID = activeAgentID
	c.agents[activeAgentID] = agent
	return c, nil
}

// Run implements Coordinator.
func (c *coordinator) Run(ctx context.Context, sessionID string, prompt string, attachments ...message.Attachment) (*fantasy.AgentResult, error) {
	if err := c.readyWg.Wait(); err != nil {
		return nil, err
	}

	// ── session logging (self-evolution data) ───────────────────────────────
	sl, _ := sessionlog.NewLogger(c.sessionLogDir, sessionID)
	if sl != nil {
		sl.LogUser("session_start", prompt, sessionlog.Meta{AgentID: c.activeAgentID})
		defer func() {
			sl.LogRuntime("session_end", "turn completed", sessionlog.Meta{AgentID: c.activeAgentID})
			sl.Close()
		}()
	}
	_ = sl // used below in sub-agent and transfer paths via closure

	// Attach the session logger to ctx so agent callbacks (OnToolResult,
	// OnToolCall, OnStepFinish) can record tool calls, reasoning, and errors
	// without threading the logger through every signature.
	if sl != nil {
		ctx = context.WithValue(ctx, toolutil.SessionLoggerContextKeyVal, sessionLogSink{l: sl})
	}

	// refresh models before each run
	if err := c.UpdateModels(ctx); err != nil {
		return nil, fmt.Errorf("failed to update models: %w", err)
	}

	model := c.currentAgent.Model()
	maxTokens := model.CatwalkCfg.DefaultMaxTokens
	if model.ModelCfg.MaxTokens != 0 {
		maxTokens = model.ModelCfg.MaxTokens
	}

	if !model.CatwalkCfg.SupportsImages && attachments != nil {
		// filter out image attachments
		filteredAttachments := make([]message.Attachment, 0, len(attachments))
		for _, att := range attachments {
			if att.IsText() {
				filteredAttachments = append(filteredAttachments, att)
			}
		}
		attachments = filteredAttachments
	}

	providerCfg, ok := c.cfg.Config().Providers.Get(model.ModelCfg.Provider)
	if !ok {
		return nil, errModelProviderNotConfigured
	}

	mergedOptions, temp, topP, topK, freqPenalty, presPenalty := mergeCallOptions(model, providerCfg)

	if providerCfg.OAuthToken != nil && providerCfg.OAuthToken.IsExpired() {
		slog.Debug("Token needs to be refreshed", "provider", providerCfg.ID)
		if err := c.refreshOAuth2Token(ctx, providerCfg); err != nil {
			// NOTE(@andreynering): We don't return here because the event handling to ask the user to reauthenticate
			// depends on the flow below. If refresh fails, proceed with the token we have.
			slog.Error("Failed to refresh OAuth2 token. Proceeding with existing token.", "error", err)
		}
	}

	// Push model: fold background-job completion notifications into this
	// turn's prompt so the model learns jobs finished without polling
	// job_output (see shell.DrainCompletedNotifications).
	if jobNotes := shell.GetBackgroundShellManager().DrainCompletedNotifications(sessionID); len(jobNotes) > 0 {
		prompt = jobs.FormatPendingJobNotifications(jobNotes) + "\n\n" + prompt
	}

	// Inject session context (branch/snapshot info) into system prompt.
	if sessionCtx := getSessionContext(ctx, c.sessions, sessionID); sessionCtx != "" {
		c.currentAgent.SetSystemPrompt(c.currentAgent.SystemPrompt() + "\n\n<session_context>\n" + sessionCtx + "\n</session_context>")
	}

	run := func() (*fantasy.AgentResult, error) {
		return c.currentAgent.Run(ctx, SessionAgentCall{
			SessionID:        sessionID,
			Prompt:           prompt,
			Attachments:      attachments,
			MaxOutputTokens:  maxTokens,
			ProviderOptions:  mergedOptions,
			Temperature:      temp,
			TopP:             topP,
			TopK:             topK,
			FrequencyPenalty: freqPenalty,
			PresencePenalty:  presPenalty,
		})
	}
	beforeLoaded := c.skillTracker.LoadedNames()
	var result *fantasy.AgentResult
	var originalErr error
	result, originalErr = run()
	defer c.drainQueuedSummaries()
	logTurnSkillUsage(sessionID, prompt, c.activeSkills, c.skillTracker, beforeLoaded)

	if c.isUnauthorized(originalErr) {
		switch {
		case providerCfg.OAuthToken != nil:
			slog.Debug("Received 401. Refreshing token and retrying", "provider", providerCfg.ID)
			if err := c.refreshOAuth2Token(ctx, providerCfg); err != nil {
				return nil, originalErr
			}
			slog.Debug("Retrying request with refreshed OAuth token", "provider", providerCfg.ID)
			return run()
		case strings.Contains(providerCfg.APIKeyTemplate, "$"):
			slog.Debug("Received 401. Refreshing API Key template and retrying", "provider", providerCfg.ID)
			if err := c.refreshApiKeyTemplate(ctx, providerCfg); err != nil {
				return nil, originalErr
			}
			slog.Debug("Retrying request with refreshed API key", "provider", providerCfg.ID)
			return run()
		}
	}

	return result, originalErr
}

// sessionLogSink adapts *sessionlog.Logger to toolutil.SessionLoggerSink.
// It lives in the coordinator (not the tool layer) so toolutil stays free of
// a sessionlog import.
type sessionLogSink struct{ l *sessionlog.Logger }

// SetMessenger wires the external-account send port (e.g. messaging
// integration) used by the coordinator-owned messaging tools. A nil m falls
// back to NoopMessenger.
func (c *coordinator) SetMessenger(m messenger.Messenger) {
	if m == nil {
		m = messenger.NoopMessenger{}
	}
	c.messenger = m
}

// SetMainAgent switches the active agent to the given agent/mode ID.
// It builds the target agent's system prompt and tools asynchronously
// via readyWg so that the next Run() call waits for them to be ready.
func (c *coordinator) SetMainAgent(agentID string) error {
	if agentID == c.activeAgentID {
		return nil
	}

	fromAgent := c.activeAgentID
	agentCfg, ok := c.cfg.Config().Agents[agentID]
	if !ok {
		return fmt.Errorf("agent %q not configured", agentID)
	}

	promptOpts := []prompt.Option{prompt.WithWorkingDir(c.cfg.WorkingDir())}
	agentPrompt, err := prompt.PromptForAgent(agentCfg, promptOpts...)
	if err != nil {
		return err
	}

	agent, err := c.buildAgent(context.Background(), agentPrompt, agentCfg, false)
	if err != nil {
		return err
	}

	c.currentAgent = agent
	c.activeAgentID = agentID
	c.agents[agentID] = agent

	slog.Info("Agent switched", "from", fromAgent, "to", agentID)
	return nil
}

func (c *coordinator) Model() Model {
	return c.currentAgent.Model()
}

func (c *coordinator) ActiveAgentID() string {
	return c.activeAgentID
}

// ActiveAgentSystemPrompt returns the proven system prompt of the active
// agent, or "" when no agent is active. The /evo mode captures this at enter
// time as the stable base for optimal-theory reconstruction.
func (c *coordinator) ActiveAgentSystemPrompt() string {
	if c.currentAgent == nil {
		return ""
	}
	return c.currentAgent.SystemPrompt()
}

// SmallLanguageModel returns the configured small model for cheap auxiliary
// calls (the /evo lesson distiller). It ensures models are built first, then
// returns nil when no small model is configured or no agent is active.
func (c *coordinator) SmallLanguageModel(ctx context.Context) fantasy.LanguageModel {
	if c.currentAgent == nil {
		return nil
	}
	_ = c.UpdateModels(ctx)
	return c.currentAgent.SmallModel().Model
}

func (c *coordinator) UpdateModels(ctx context.Context) error {
	// build the models again so we make sure we get the latest config
	large, small, err := c.buildAgentModels(ctx, false)
	if err != nil {
		return err
	}
	c.currentAgent.SetModels(large, small)

	// Resolve active agent ID for tool filtering.
	activeAgentID := config.AgentDefault
	if c.cfg.Config().Options != nil && c.cfg.Config().Options.ActiveMode != "" {
		activeAgentID = c.cfg.Config().Options.ActiveMode
	}

	agentCfg, ok := c.cfg.Config().Agents[activeAgentID]
	if !ok {
		agentCfg, ok = c.cfg.Config().Agents[config.AgentDefault]
		if !ok {
			return errDefaultAgentNotConfigured
		}
		activeAgentID = config.AgentDefault
	}

	// If the active mode changed, rebuild and set the system prompt.
	if activeAgentID != c.activeAgentID {
		promptOpts := []prompt.Option{prompt.WithWorkingDir(c.cfg.WorkingDir())}
		agentPrompt, err := prompt.PromptForAgent(agentCfg, promptOpts...)
		if err != nil {
			return err
		}
		systemPrompt, err := agentPrompt.Build(ctx, large.Model.Provider(), large.Model.Model(), c.cfg)
		if err != nil {
			return err
		}
		c.currentAgent.SetSystemPrompt(systemPrompt)
		c.activeAgentID = activeAgentID
	}

	tools, err := c.buildTools(ctx, agentCfg, false)
	if err != nil {
		return err
	}
	c.currentAgent.SetTools(tools)
	return nil
}

// subAgentParams holds the parameters for running a sub-agent.
type subAgentParams struct {
	Agent          SessionAgent
	SessionID      string
	AgentMessageID string
	ToolCallID     string
	Prompt         string
	SessionTitle   string
	// AgentID is an optional instance identifier for the sub-agent.
	// When non-empty, it is propagated to the SubagentCompleted event so
	// that listeners (e.g. the web UI) can address the run uniquely.
	AgentID string
	// SubagentType is the built-in type label (e.g. "coder" / "explore"
	// / "plan") or a custom agent name. It is forwarded to the
	// SubagentCompleted event for UI styling.
	SubagentType string
	// SessionSetup is an optional callback invoked after session creation
	// but before agent execution, for custom session configuration.
	SessionSetup func(sessionID string)
}

// subAgentResult wraps a sub-agent ToolResponse with the timing and usage
// metadata collected during the run. Callers use this to populate the
// TaskResult envelope (duration_ms, usage) without having to re-derive the
// values from session state.
type subAgentResult struct {
	Response   fantasy.ToolResponse
	DurationMs int64
	Usage      fantasy.Usage
}
