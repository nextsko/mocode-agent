package agent

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"golang.org/x/sync/errgroup"

	"github.com/package-register/mocode/internal/core/agent/candidate"
	"github.com/package-register/mocode/internal/core/agent/evolution"
	"github.com/package-register/mocode/internal/core/agent/extension"
	"github.com/package-register/mocode/internal/core/agent/failover"
	"github.com/package-register/mocode/internal/core/agent/notify"
	"github.com/package-register/mocode/internal/core/agent/prompt"
	"github.com/package-register/mocode/internal/core/agent/tools"
	"github.com/package-register/mocode/internal/core/agent/tools/lsp"
	"github.com/package-register/mocode/internal/core/agent/toolutil"
	"github.com/package-register/mocode/internal/core/config"
	"github.com/package-register/mocode/internal/core/evaluation/llmjudge"
	"github.com/package-register/mocode/internal/core/hooks"
	"github.com/package-register/mocode/internal/core/knowledge/memory"
	"github.com/package-register/mocode/internal/core/permission"
	"github.com/package-register/mocode/internal/core/shellruntime/screencap"
	"github.com/package-register/mocode/internal/core/skills"
	"github.com/package-register/mocode/internal/domain/filetracker"
	"github.com/package-register/mocode/internal/domain/history"
	"github.com/package-register/mocode/internal/domain/messenger"
	"github.com/package-register/mocode/internal/domain/session"
	"github.com/package-register/mocode/internal/domain/session/message"
	"github.com/package-register/mocode/internal/domain/session/sessionlog"
	"github.com/package-register/mocode/internal/store"
	"github.com/package-register/mocode/internal/util/csync"
	"github.com/package-register/mocode/internal/util/errcoll"
	"github.com/package-register/mocode/internal/util/infra"
	"github.com/package-register/mocode/internal/util/pubsub"

	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/azure"
	"charm.land/fantasy/providers/google"
	"charm.land/fantasy/providers/openai"
	"charm.land/fantasy/providers/openaicompat"
	"charm.land/fantasy/providers/openrouter"
	"charm.land/fantasy/providers/vercel"
	"github.com/qjebbs/go-jsons"
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

type Coordinator interface {
	// SetMainAgent switches the active agent to the given mode/agent ID.
	// This supports transfer_to_agent and mode switching.
	SetMainAgent(agentID string) error
	// SetMessenger wires the external-account send port (e.g. WeChat) that
	// the coordinator-owned messaging tools use. Safe to call once at startup.
	SetMessenger(m messenger.Messenger)
	// SetExtensions wires the on_xxx lifecycle hook manager. The /evo mode
	// uses this to register an observability extension that folds successful
	// runs into the optimal-theory prompt. Safe to call once at startup.
	SetExtensions(m *extension.Manager)
	// RegisterExtension adds an extension to the lifecycle manager and
	// enables it. Used by the /evo mode to attach its observability
	// extension without owning the manager.
	RegisterExtension(ext extension.Extension)
	// UnregisterExtension removes an extension by name (e.g. on /evo exit).
	UnregisterExtension(name string)
	Run(ctx context.Context, sessionID, prompt string, attachments ...message.Attachment) (*fantasy.AgentResult, error)
	Cancel(sessionID string)
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
}

type coordinator struct {
	cfg         *config.ConfigStore
	sessions    session.Service
	messages    message.Service
	permissions permission.Service
	history     history.Service
	filetracker filetracker.Service
	lspManager  *lsp.Manager
	memory      memory.Service
	notify      pubsub.Publisher[notify.Notification]

	currentAgent  SessionAgent
	activeAgentID string
	agents        map[string]SessionAgent
	// subagentIndex maps the user-visible sub-agent ID (params.AgentID,
	// e.g. "<parentToolCallID>-1") to the internal sub-session ID used by
	// sessionAgent.Cancel. The mapping is populated by runSubAgentWithMeta
	// before the sub-agent goroutine starts and removed in a defer so
	// that cancelled sub-agents can be stopped without affecting the
	// parent session.
	subagentIndex  *csync.Map[string, string]
	summaryQueue   *sessionSummaryQueue
	sessionLogDir  string // base dir for session logs
	errorLearner   *evolution.ErrorLearner
	errorCollector *errcoll.Collector
	sessionSearch  *store.SessionSearch
	// messenger is the external-account send port (e.g. WeChat). Defaults to
	// NoopMessenger; wired from the composition root after construction.
	messenger messenger.Messenger
	// extensions holds the on_xxx lifecycle hook manager. Defaults to a
	// no-op manager; the /evo mode wires an observability extension here so
	// runs/tools emit events without per-agent configuration.
	extensions *extension.Manager
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
	history history.Service,
	filetracker filetracker.Service,
	lspManager *lsp.Manager,
	memory memory.Service,
	notify pubsub.Publisher[notify.Notification],
	errorCollector *errcoll.Collector,
	sessionSearch *store.SessionSearch,
) (Coordinator, error) {
	// Discover skills once at session start.
	allSkills, activeSkills := discoverSkills(cfg)
	skillTracker := skills.NewTracker(activeSkills)

	// cfg.Config().Options.DataDirectory is always resolved to an absolute
	// path by config.setDefaults, so it can be used directly. Joining it
	// with cfg.WorkingDir() would produce a path with two drive letters
	// on Windows (e.g. "C:\wd\C:\wd\.mocode\sessions").
	dataDir := ".mocode"
	if cfg.Config().Options != nil && cfg.Config().Options.DataDirectory != "" {
		dataDir = cfg.Config().Options.DataDirectory
	}

	c := &coordinator{
		cfg:            cfg,
		sessions:       sessions,
		messages:       messages,
		permissions:    permissions,
		history:        history,
		filetracker:    filetracker,
		lspManager:     lspManager,
		memory:         memory,
		notify:         notify,
		agents:         make(map[string]SessionAgent),
		subagentIndex:  csync.NewMap[string, string](),
		summaryQueue:   sessionSummaryQueueNew(),
		allSkills:      allSkills,
		activeSkills:   activeSkills,
		skillTracker:   skillTracker,
		sessionLogDir:  filepath.Join(dataDir, "sessions"),
		errorCollector: errorCollector,
		sessionSearch:  sessionSearch,
	}

	// Initialize error learner for provider-specific mistake tracking.
	if learner, learnerErr := evolution.NewErrorLearner(filepath.Join(cfg.WorkingDir(), ".mocode", "evolution", "patterns")); learnerErr == nil {
		c.errorLearner = learner
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
	agentPrompt, err := promptForAgent(agentCfg, promptOpts...)
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

	// Extension lifecycle: BeforeRun lets extensions observe (or abort via a
	// Decision) every agent run. Dispatched before any work so observers see
	// the prompt that drives the turn.
	if dec := c.fireExtensions(ctx, extension.EventBeforeRun, &extension.Context{
		SessionID: sessionID, AgentName: c.activeAgentID, Prompt: prompt,
	}); dec != nil && dec.AbortRun {
		// Honor the guardrail: a BeforeRun abort stops the run before any work
		// begins. This is the only safe abort point — after work starts the
		// agent owns the turn. Surfaced as a typed error so callers can tell a
		// guardrail stop from a real failure.
		return nil, fmt.Errorf("run aborted by extension: %s", dec.AbortReason)
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

	// Inject session context (branch/snapshot info) into system prompt.
	if sessionCtx := getSessionContext(ctx, c.sessions, sessionID); sessionCtx != "" {
		c.currentAgent.SetSystemPrompt(c.currentAgent.SystemPrompt() + "\n\n<session_context>\n" + sessionCtx + "\n</session_context>")
	}

	// Inject self-evolution patches into system prompt.
	c.injectEvolutionContext(ctx)

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
	if agentCfg := c.activeAgentConfig(); agentCfg != nil && agentCfg.BestOfN > 1 {
		result, originalErr = c.runBestOfN(ctx, sessionID, prompt, maxTokens, mergedOptions, temp, topP, topK, freqPenalty, presPenalty, attachments, agentCfg.BestOfN)
	} else {
		result, originalErr = run()
	}
	defer c.drainQueuedSummaries()
	logTurnSkillUsage(sessionID, prompt, c.activeSkills, c.skillTracker, beforeLoaded)

	// Extension lifecycle: AfterRun or OnError depending on the outcome.
	// Observers (the /evo loop) use this to fold successful turns into the
	// optimal-theory prompt and capture failures as evolution data.
	runEvt := extension.EventAfterRun
	if originalErr != nil {
		runEvt = extension.EventOnError
	}
	c.fireExtensions(ctx, runEvt, &extension.Context{
		SessionID: sessionID, AgentName: c.activeAgentID, Prompt: prompt,
		Messages: resultMessages(result), Response: resultResponseText(result),
		ToolNames: resultToolNames(result), Err: originalErr,
	})

	// Effect loop: after a successful turn, asynchronously score it via an LLM
	// judge and record the score into sessionlog. This feeds the positive
	// (effect) side of the evolution loop — low scores become evolution data.
	// Best-effort: failures are silent, never affect the returned result.
	if result != nil && originalErr == nil {
		go c.scoreTurn(sessionID, prompt)
	}

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

func getProviderOptions(model Model, providerCfg config.ProviderConfig) fantasy.ProviderOptions {
	options := fantasy.ProviderOptions{}

	cfgOpts := []byte("{}")
	providerCfgOpts := []byte("{}")
	catwalkOpts := []byte("{}")

	if model.ModelCfg.ProviderOptions != nil {
		data, err := json.Marshal(model.ModelCfg.ProviderOptions)
		if err == nil {
			cfgOpts = data
		}
	}

	if providerCfg.ProviderOptions != nil {
		data, err := json.Marshal(providerCfg.ProviderOptions)
		if err == nil {
			providerCfgOpts = data
		}
	}

	if model.CatwalkCfg.Options.ProviderOptions != nil {
		data, err := json.Marshal(model.CatwalkCfg.Options.ProviderOptions)
		if err == nil {
			catwalkOpts = data
		}
	}

	readers := []io.Reader{
		bytes.NewReader(catwalkOpts),
		bytes.NewReader(providerCfgOpts),
		bytes.NewReader(cfgOpts),
	}

	got, err := jsons.Merge(readers)
	if err != nil {
		slog.Error("Could not merge call config", "err", err)
		return options
	}

	mergedOptions := make(map[string]any)

	err = json.Unmarshal([]byte(got), &mergedOptions)
	if err != nil {
		slog.Error("Could not create config for call", "err", err)
		return options
	}

	switch providerCfg.Type {
	case openai.Name, azure.Name:
		_, hasReasoningEffort := mergedOptions["reasoning_effort"]
		if !hasReasoningEffort && model.ModelCfg.ReasoningEffort != "" {
			mergedOptions["reasoning_effort"] = model.ModelCfg.ReasoningEffort
		}
		if openai.IsResponsesModel(model.CatwalkCfg.ID) {
			if openai.IsResponsesReasoningModel(model.CatwalkCfg.ID) {
				mergedOptions["reasoning_summary"] = "auto"
				mergedOptions["include"] = []openai.IncludeType{openai.IncludeReasoningEncryptedContent}
			}
			parsed, err := openai.ParseResponsesOptions(mergedOptions)
			if err == nil {
				options[openai.Name] = parsed
			}
		} else {
			parsed, err := openai.ParseOptions(mergedOptions)
			if err == nil {
				options[openai.Name] = parsed
			}
		}
	case anthropic.Name:
		var (
			_, hasEffort = mergedOptions["effort"]
			_, hasThink  = mergedOptions["thinking"]
		)
		switch {
		case !hasEffort && model.ModelCfg.ReasoningEffort != "":
			mergedOptions["effort"] = model.ModelCfg.ReasoningEffort
		case !hasThink && model.ModelCfg.Think:
			mergedOptions["thinking"] = map[string]any{"budget_tokens": 2000}
		}
		parsed, err := anthropic.ParseOptions(mergedOptions)
		if err == nil {
			options[anthropic.Name] = parsed
		}

	case openrouter.Name:
		_, hasReasoning := mergedOptions["reasoning"]
		if !hasReasoning && model.ModelCfg.ReasoningEffort != "" {
			mergedOptions["reasoning"] = map[string]any{
				"enabled": true,
				"effort":  model.ModelCfg.ReasoningEffort,
			}
		}
		parsed, err := openrouter.ParseOptions(mergedOptions)
		if err == nil {
			options[openrouter.Name] = parsed
		}
	case vercel.Name:
		_, hasReasoning := mergedOptions["reasoning"]
		if !hasReasoning && model.ModelCfg.ReasoningEffort != "" {
			mergedOptions["reasoning"] = map[string]any{
				"enabled": true,
				"effort":  model.ModelCfg.ReasoningEffort,
			}
		}
		parsed, err := vercel.ParseOptions(mergedOptions)
		if err == nil {
			options[vercel.Name] = parsed
		}
	case google.Name:
		_, hasReasoning := mergedOptions["thinking_config"]
		if !hasReasoning {
			if strings.HasPrefix(model.CatwalkCfg.ID, "gemini-2") {
				mergedOptions["thinking_config"] = map[string]any{
					"thinking_budget":  2000,
					"include_thoughts": true,
				}
			} else {
				mergedOptions["thinking_config"] = map[string]any{
					"thinking_level":   model.ModelCfg.ReasoningEffort,
					"include_thoughts": true,
				}
			}
		}
		parsed, err := google.ParseOptions(mergedOptions)
		if err == nil {
			options[google.Name] = parsed
		}
	case openaicompat.Name:
		_, hasReasoningEffort := mergedOptions["reasoning_effort"]
		if !hasReasoningEffort && model.ModelCfg.ReasoningEffort != "" {
			mergedOptions["reasoning_effort"] = model.ModelCfg.ReasoningEffort
		}

		extraBody := make(map[string]any)

		// "reasoning effort" is a standard OpenAI field, but "thinking" is not.
		// Setting it in the right way for each provider.
		// TODO: Abstract this in Fantasy somehow?
		// TODO: Allow custom providers to specify how to set this?
		switch providerCfg.ID {
		case string(catwalk.InferenceProviderIoNet):
			extraBody["chat_template_kwargs"] = map[string]any{
				"thinking": model.ModelCfg.Think,
			}
		case string(catwalk.InferenceProviderZAI):
			if model.ModelCfg.Think {
				extraBody["thinking"] = map[string]any{
					"type": "enabled",
				}
			} else {
				extraBody["thinking"] = map[string]any{
					"type": "disabled",
				}
			}
		}

		mergedOptions["extra_body"] = extraBody

		parsed, err := openaicompat.ParseOptions(mergedOptions)
		if err == nil {
			options[openaicompat.Name] = parsed
		}
	}

	return options
}

func mergeCallOptions(model Model, cfg config.ProviderConfig) (fantasy.ProviderOptions, *float64, *float64, *int64, *float64, *float64) {
	modelOptions := getProviderOptions(model, cfg)
	temp := cmp.Or(model.ModelCfg.Temperature, model.CatwalkCfg.Options.Temperature)
	topP := cmp.Or(model.ModelCfg.TopP, model.CatwalkCfg.Options.TopP)
	topK := cmp.Or(model.ModelCfg.TopK, model.CatwalkCfg.Options.TopK)
	freqPenalty := cmp.Or(model.ModelCfg.FrequencyPenalty, model.CatwalkCfg.Options.FrequencyPenalty)
	presPenalty := cmp.Or(model.ModelCfg.PresencePenalty, model.CatwalkCfg.Options.PresencePenalty)
	return modelOptions, temp, topP, topK, freqPenalty, presPenalty
}

func (c *coordinator) buildAgent(ctx context.Context, prompt *prompt.Prompt, agent config.Agent, isSubAgent bool) (SessionAgent, error) {
	large, small, err := c.buildAgentModels(ctx, isSubAgent)
	if err != nil {
		return nil, err
	}

	largeProviderCfg, _ := c.cfg.Config().Providers.Get(large.ModelCfg.Provider)
	result := NewSessionAgent(SessionAgentOptions{
		LargeModel:           large,
		SmallModel:           small,
		SystemPromptPrefix:   largeProviderCfg.SystemPromptPrefix,
		SystemPrompt:         "",
		IsSubAgent:           isSubAgent,
		DisableAutoSummarize: c.cfg.Config().Options.DisableAutoSummarize,
		IsYolo:               c.permissions.SkipRequests(),
		Sessions:             c.sessions,
		Messages:             c.messages,
		Tools:                nil,
		Notify:               c.notify,
		Memory:               c.memory,
		WorkingDir:           c.cfg.WorkingDir(),
		ErrorLearner:         c.errorLearner,
		ErrorCollector:       c.errorCollector,
	})

	c.readyWg.Go(func() error {
		systemPrompt, err := prompt.Build(ctx, large.Model.Provider(), large.Model.Model(), c.cfg)
		if err != nil {
			return err
		}
		result.SetSystemPrompt(systemPrompt)
		return nil
	})

	c.readyWg.Go(func() error {
		tools, err := c.buildTools(ctx, agent, isSubAgent)
		if err != nil {
			return err
		}
		result.SetTools(tools)
		return nil
	})

	return result, nil
}

func (c *coordinator) buildTools(ctx context.Context, agentCfg config.Agent, isSubAgent bool) ([]fantasy.AgentTool, error) {
	// ── coordinator-owned tools (call back into coordinator state) ───────────
	var allTools []fantasy.AgentTool
	if slices.Contains(agentCfg.AllowedTools, AgentToolName) {
		agentTool, err := c.agentTool(ctx)
		if err != nil {
			return nil, err
		}
		allTools = append(allTools, agentTool)
	}
	if slices.Contains(agentCfg.AllowedTools, tools.AgenticFetchToolName) {
		agenticFetchTool, err := c.agenticFetchTool(ctx, nil)
		if err != nil {
			return nil, err
		}
		allTools = append(allTools, agenticFetchTool)
	}
	if slices.Contains(agentCfg.AllowedTools, RoundtableToolName) {
		roundtableTool, err := c.roundtableTool(ctx)
		if err != nil {
			return nil, err
		}
		allTools = append(allTools, roundtableTool)
	}

	// ── standard tools via registry ──────────────────────────────────────────
	modelName := ""
	if modelCfg, ok := c.cfg.Config().Models[agentCfg.Model]; ok {
		if m := c.cfg.Config().GetModel(modelCfg.Provider, modelCfg.Model); m != nil {
			modelName = m.Name
		}
	}
	deps := tools.ToolDeps{
		Cfg:          c.cfg,
		Permissions:  c.permissions,
		LSPManager:   c.lspManager,
		History:      c.history,
		FileTracker:  c.filetracker,
		Sessions:     c.sessions,
		Messages:     c.messages,
		Memory:       c.memory,
		AllSkills:    c.allSkills,
		ActiveSkills: c.activeSkills,
		SkillTracker: c.skillTracker,
		ModelName:    modelName,
		SummarySchedule: func(ctx context.Context, sessionID string) error {
			c.summaryQueue.Add(sessionID)
			return nil
		},
		SessionSearch: c.sessionSearch,
	}
	allTools = append(allTools, tools.NewRegistry().Build(ctx, deps)...)

	// ── transfer_to_agent (coordinator-owned, config-driven) ─────────────────
	if len(agentCfg.SubAgents) > 0 {
		onTransfer := func(ctx context.Context, fromAgent, toAgent, message string) error {
			// Validate the target is in the sub_agents list.
			found := false
			for _, sa := range agentCfg.SubAgents {
				if sa == toAgent {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("agent %q is not in sub_agents list", toAgent)
			}
			// Actually switch the coordinator's active agent.
			if err := c.SetMainAgent(toAgent); err != nil {
				return fmt.Errorf("failed to switch to agent %q: %w", toAgent, err)
			}
			return nil
		}
		allTools = append(allTools, tools.NewTransferTool(agentCfg.SubAgents, onTransfer))
	}

	// ── Screenshot tools ─────────────────────────────────────────────────────
	screenshotDir := filepath.Join(c.cfg.WorkingDir(), ".mocode", "screenshots")
	allTools = append(allTools, screencap.NewAgentTool(screenshotDir))

	// ── WeChat tools (coordinator-owned, always available regardless of AllowedTools) ─
	messengerPort := c.messenger
	if messengerPort == nil {
		messengerPort = messenger.NoopMessenger{}
	}
	allTools = append(
		allTools,
		tools.NewWeChatSendImageTool(messengerPort),
		tools.NewWeChatSendFileTool(messengerPort),
		tools.NewWeChatScreenshotTool(messengerPort, screenshotDir),
	)

	// ── build hook runner if PreToolUse hooks are configured ─────────────────
	var hookRunner *hooks.Runner
	if preToolHooks := c.cfg.Config().Hooks[hooks.EventPreToolUse]; len(preToolHooks) > 0 {
		hookRunner = hooks.NewRunner(preToolHooks, c.cfg.WorkingDir(), c.cfg.WorkingDir())
	}

	// ── filter by AllowedTools ────────────────────────────────────────────────
	// coordinator-owned tools (agent, agentic_fetch, transfer_to_agent) bypass
	// the AllowedTools filter because they are already conditioned at the top.
	coordOwned := map[string]bool{
		AgentToolName:                  true,
		tools.AgenticFetchToolName:     true,
		RoundtableToolName:             true,
		tools.TransferToolName:         true,
		tools.WeChatSendImageToolName:  true,
		tools.WeChatSendFileToolName:   true,
		tools.WeChatScreenshotToolName: true,
	}
	var filteredTools []fantasy.AgentTool
	for _, tool := range allTools {
		if coordOwned[tool.Info().Name] || slices.Contains(agentCfg.AllowedTools, tool.Info().Name) {
			filteredTools = append(filteredTools, tool)
		}
	}

	// ── append runtime MCP tools with AllowedMCP filter ──────────────────────
	for _, mcpTool := range tools.GetMCPTools(c.permissions, c.cfg, c.cfg.WorkingDir()) {
		if agentCfg.AllowedMCP == nil {
			// No MCP restrictions.
			filteredTools = append(filteredTools, mcpTool)
			continue
		}
		if len(agentCfg.AllowedMCP) == 0 {
			// No MCPs allowed.
			slog.Debug("No MCPs allowed", "tool", mcpTool.Name(), "agent", agentCfg.Name)
			break
		}
		for mcpName, mcpToolNames := range agentCfg.AllowedMCP {
			if mcpName != mcpTool.MCP() {
				continue
			}
			if len(mcpToolNames) == 0 || slices.Contains(mcpToolNames, mcpTool.MCPToolName()) {
				filteredTools = append(filteredTools, mcpTool)
				break
			}
			slog.Debug("MCP not allowed", "tool", mcpTool.Name(), "agent", agentCfg.Name)
		}
	}
	slices.SortFunc(filteredTools, func(a, b fantasy.AgentTool) int {
		return strings.Compare(a.Info().Name, b.Info().Name)
	})

	// Wrap tools with hook interception for the top-level agent only.
	// Sub-agents (the `agent` task tool, `agentic_fetch`, etc.) run
	// without hook interception to avoid firing the user's hook N times
	// per delegated turn. The top-level invocation of the sub-agent tool
	// itself is still wrapped from the coder's side.
	filteredTools = wrapToolsWithHooks(filteredTools, hookRunner, isSubAgent)

	return filteredTools, nil
}

// TODO: when we support multiple agents we need to change this so that we pass in the agent specific model config
func (c *coordinator) buildAgentModels(ctx context.Context, isSubAgent bool) (Model, Model, error) {
	largeModelCfg, ok := c.cfg.Config().Models[config.SelectedModelTypeLarge]
	if !ok {
		return Model{}, Model{}, errLargeModelNotSelected
	}
	smallModelCfg, ok := c.cfg.Config().Models[config.SelectedModelTypeSmall]
	if !ok {
		return Model{}, Model{}, errSmallModelNotSelected
	}

	largeProviderCfg, ok := c.cfg.Config().Providers.Get(largeModelCfg.Provider)
	if !ok {
		return Model{}, Model{}, errLargeModelProviderNotConfigured
	}

	largeProvider, err := c.buildProvider(largeProviderCfg, largeModelCfg, isSubAgent)
	if err != nil {
		return Model{}, Model{}, err
	}

	smallProviderCfg, ok := c.cfg.Config().Providers.Get(smallModelCfg.Provider)
	if !ok {
		return Model{}, Model{}, errSmallModelProviderNotConfigured
	}

	smallProvider, err := c.buildProvider(smallProviderCfg, smallModelCfg, true)
	if err != nil {
		return Model{}, Model{}, err
	}

	var largeCatwalkModel *catwalk.Model
	var smallCatwalkModel *catwalk.Model

	for _, m := range largeProviderCfg.Models {
		if m.ID == largeModelCfg.Model {
			largeCatwalkModel = &m
		}
	}
	for _, m := range smallProviderCfg.Models {
		if m.ID == smallModelCfg.Model {
			smallCatwalkModel = &m
		}
	}

	if largeCatwalkModel == nil {
		return Model{}, Model{}, errLargeModelNotFound
	}

	if smallCatwalkModel == nil {
		return Model{}, Model{}, errSmallModelNotFound
	}

	largeModelID := largeModelCfg.Model
	smallModelID := smallModelCfg.Model

	if largeModelCfg.Provider == openrouter.Name && isExactoSupported(largeModelID) {
		largeModelID += ":exacto"
	}

	if smallModelCfg.Provider == openrouter.Name && isExactoSupported(smallModelID) {
		smallModelID += ":exacto"
	}

	largeModel, err := largeProvider.LanguageModel(ctx, largeModelID)
	if err != nil {
		return Model{}, Model{}, err
	}
	smallModel, err := smallProvider.LanguageModel(ctx, smallModelID)
	if err != nil {
		return Model{}, Model{}, err
	}

	// Wrap with failover when a fallback is declared. The wrap is a no-op when
	// no fallback is configured, so existing behavior is unchanged.
	largeModel, err = c.withFailover(ctx, largeModel, largeModelCfg)
	if err != nil {
		return Model{}, Model{}, err
	}
	smallModel, err = c.withFailover(ctx, smallModel, smallModelCfg)
	if err != nil {
		return Model{}, Model{}, err
	}

	return Model{
			Model:      largeModel,
			CatwalkCfg: *largeCatwalkModel,
			ModelCfg:   largeModelCfg,
		}, Model{
			Model:      smallModel,
			CatwalkCfg: *smallCatwalkModel,
			ModelCfg:   smallModelCfg,
		}, nil
}

// withFailover wraps a primary LanguageModel with failover when the model's
// config declares a Fallback. It mirrors the primary construction (resolve
// provider config, build provider, resolve model id) for the fallback, then
// wraps the two with failover.New. When no fallback is declared the primary is
// returned unchanged, preserving existing behavior.
func (c *coordinator) withFailover(ctx context.Context, primary fantasy.LanguageModel, cfg config.SelectedModel) (fantasy.LanguageModel, error) {
	if cfg.Fallback == nil {
		return primary, nil
	}
	fb := *cfg.Fallback
	fbProviderCfg, ok := c.cfg.Config().Providers.Get(fb.Provider)
	if !ok {
		return nil, fmt.Errorf("failover: fallback provider %q not configured", fb.Provider)
	}
	fbProvider, err := c.buildProvider(fbProviderCfg, fb, true)
	if err != nil {
		return nil, fmt.Errorf("failover: build fallback provider: %w", err)
	}
	fbModelID := fb.Model
	if fb.Provider == openrouter.Name && isExactoSupported(fbModelID) {
		fbModelID += ":exacto"
	}
	fbModel, err := fbProvider.LanguageModel(ctx, fbModelID)
	if err != nil {
		return nil, fmt.Errorf("failover: build fallback model: %w", err)
	}
	wrapped, err := failover.New(failover.WithPrimary(primary), failover.WithFallback(fbModel))
	if err != nil {
		return nil, fmt.Errorf("failover: %w", err)
	}
	return wrapped, nil
}

// sessionLogSink adapts *sessionlog.Logger to toolutil.SessionLoggerSink.
// It lives in the coordinator (not the tool layer) so toolutil stays free of
// a sessionlog import.
type sessionLogSink struct{ l *sessionlog.Logger }

func (s sessionLogSink) LogToolCall(event, data string, m toolutil.SessionLogMeta) {
	s.l.LogToolCall(event, data, toSessionLogMeta(m))
}

func (s sessionLogSink) LogThink(event, data string, m toolutil.SessionLogMeta) {
	s.l.LogThink(event, data, toSessionLogMeta(m))
}

func (s sessionLogSink) LogBug(event, data string, m toolutil.SessionLogMeta) {
	s.l.LogBug(event, data, toSessionLogMeta(m))
}

func (s sessionLogSink) LogInfo(event, data string, m toolutil.SessionLogMeta) {
	s.l.LogInfo(event, data, toSessionLogMeta(m))
}

func toSessionLogMeta(m toolutil.SessionLogMeta) sessionlog.Meta {
	return sessionlog.Meta{
		ToolName:   m.ToolName,
		ToolCallID: m.ToolCallID,
		AgentID:    m.AgentID,
		DurationMs: m.DurationMs,
		ErrorType:  m.ErrorType,
	}
}

// activeAgentConfig returns the config for the currently active agent, or nil
// if it cannot be resolved.
func (c *coordinator) activeAgentConfig() *config.Agent {
	agentCfg, ok := c.cfg.Config().Agents[c.activeAgentID]
	if !ok {
		agentCfg, ok = c.cfg.Config().Agents[config.AgentDefault]
		if !ok {
			return nil
		}
	}
	return &agentCfg
}

// runBestOfN generates n candidate responses in parallel (each in its own task
// session) and uses an LLM judge to select the best. It is the best-of-N path
// enabled by config.Agent.BestOfN > 1. When the judge model cannot be built,
// it degrades to a single run.
func (c *coordinator) runBestOfN(
	ctx context.Context,
	sessionID, prompt string,
	maxTokens int64,
	mergedOptions fantasy.ProviderOptions,
	temp, topP *float64,
	topK *int64,
	freqPenalty, presPenalty *float64,
	attachments []message.Attachment,
	n int,
) (*fantasy.AgentResult, error) {
	judgeModel, err := c.buildSmallLanguageModel(ctx)
	if err != nil {
		slog.Warn("best-of-n: cannot build judge model, falling back to single run", "error", err)
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
	gen := func(ctx context.Context, runID int) (candidate.Result, error) {
		taskSession, err := c.sessions.CreateTaskSession(ctx, fmt.Sprintf("bestofn-%d", runID), sessionID, fmt.Sprintf("best-of-n candidate %d", runID))
		if err != nil {
			return candidate.Result{}, err
		}
		res, err := c.currentAgent.Run(ctx, SessionAgentCall{
			SessionID:        taskSession.ID,
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
		if err != nil {
			return candidate.Result{Result: res, SessionID: taskSession.ID, Err: err}, err
		}
		trace, traceErr := c.messages.List(ctx, taskSession.ID)
		if traceErr != nil {
			trace = nil
		}
		return candidate.Result{Result: res, Trace: trace, SessionID: taskSession.ID}, nil
	}
	sel := &candidate.LLMSelector{Model: judgeModel, Prompt: prompt}
	winner, err := candidate.Run(ctx, n, gen, sel)
	if err != nil {
		return nil, err
	}
	return winner.Result, nil
}

// buildSmallLanguageModel constructs the configured small model as a
// fantasy.LanguageModel, for use as the best-of-N judge.
func (c *coordinator) buildSmallLanguageModel(ctx context.Context) (fantasy.LanguageModel, error) {
	smallModelCfg, ok := c.cfg.Config().Models[config.SelectedModelTypeSmall]
	if !ok {
		return nil, errSmallModelNotSelected
	}
	smallProviderCfg, ok := c.cfg.Config().Providers.Get(smallModelCfg.Provider)
	if !ok {
		return nil, errSmallModelProviderNotConfigured
	}
	smallProvider, err := c.buildProvider(smallProviderCfg, smallModelCfg, true)
	if err != nil {
		return nil, err
	}
	smallModelID := smallModelCfg.Model
	if smallModelCfg.Provider == openrouter.Name && isExactoSupported(smallModelID) {
		smallModelID += ":exacto"
	}
	return smallProvider.LanguageModel(ctx, smallModelID)
}

// scoreTurn asynchronously scores a completed turn with an LLM judge and
// records the result into the session log. It is the online end of the
// evaluation system: the same judging capability used offline by `mocode eval`
// here produces a per-turn quality signal that feeds the evolution loop.
//
// Best-effort and non-blocking: any failure (no small model, judge error) is
// logged at debug level and dropped. The returned turn result is unaffected.
func (c *coordinator) scoreTurn(sessionID, prompt string) {
	ctx := context.Background()
	judgeModel, err := c.buildSmallLanguageModel(ctx)
	if err != nil {
		slog.Debug("effect loop: no judge model, skipping turn scoring", "error", err)
		return
	}
	trace, err := c.messages.List(ctx, sessionID)
	if err != nil || len(trace) == 0 {
		slog.Debug("effect loop: no trace to score", "error", err)
		return
	}
	judge := &llmjudge.Judge{Model: judgeModel}
	// Score the turn against a general helpfulness rubric. This is deliberately
	// broad: the goal is a quality signal, not a task-specific pass/fail.
	res, err := judge.Judge(ctx, []string{
		"Did the response address the user's request accurately and completely?",
		"Was the response helpful, clear, and free of errors?",
	}, trace)
	if err != nil {
		slog.Debug("effect loop: judge failed", "error", err)
		return
	}
	// Record the score into the session log so the evolution producer and any
	// analysis tooling can consume it.
	if lg, _ := sessionlog.NewLogger(c.sessionLogDir, sessionID); lg != nil {
		lg.LogInfo("turn_score", fmt.Sprintf("score=%.2f reason=%s", res.Score, truncateForLogScore(res.Reason)), sessionlog.Meta{})
		lg.Close()
	}
}

func truncateForLogScore(s string) string {
	const max = 200
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func isExactoSupported(modelID string) bool {
	supportedModels := []string{
		"moonshotai/kimi-k2-0905",
		"deepseek/deepseek-v3.1-terminus",
		"z-ai/glm-4.6",
		"openai/gpt-oss-120b",
		"qwen/qwen3-coder",
	}
	return slices.Contains(supportedModels, modelID)
}

// SetMessenger wires the external-account send port (e.g. WeChat) used by the
// coordinator-owned messaging tools. A nil m falls back to NoopMessenger.
func (c *coordinator) SetMessenger(m messenger.Messenger) {
	if m == nil {
		m = messenger.NoopMessenger{}
	}
	c.messenger = m
}

// SetExtensions wires the on_xxx lifecycle hook manager. A nil manager is
// replaced with an empty one so dispatch is always a safe no-op; this lets
// callers fire events unconditionally without nil checks.
func (c *coordinator) SetExtensions(m *extension.Manager) {
	if m == nil {
		m = extension.NewManager()
	}
	c.extensions = m
}

// RegisterExtension adds an extension to the lifecycle manager, enabling it.
// Lazily allocates a manager if SetExtensions was never called.
func (c *coordinator) RegisterExtension(ext extension.Extension) {
	if c.extensions == nil {
		c.extensions = extension.NewManager()
	}
	// Reject shadowing duplicates rather than silently replacing: two extensions
	// sharing a Name would silently shadow each other, which is a wiring bug.
	// Log best-effort; the run continues unaffected (see extension.Dispatch recover).
	if err := c.extensions.RegisterOrError(ext); err != nil {
		slog.Warn("extension register rejected", "err", err)
	}
}

// UnregisterExtension removes an extension by name (e.g. on /evo exit).
func (c *coordinator) UnregisterExtension(name string) {
	if c.extensions != nil {
		c.extensions.Unregister(name)
	}
}

// fireExtensions dispatches an event to every enabled extension. Safe to call
// before SetExtensions (a nil manager is a no-op).
// fireExtensions dispatches a lifecycle event and returns the aggregated
// Decision. A non-nil Decision with AbortRun set means an extension requested
// the run stop; the caller (Run) honors it at BeforeRun by returning early
// before any work begins. At AfterRun the run is already complete, so the
// decision is only logged.
func (c *coordinator) fireExtensions(ctx context.Context, ev extension.Event, ec *extension.Context) *extension.Decision {
	if c.extensions == nil {
		return nil
	}
	dec := c.extensions.Dispatch(ctx, ev, ec)
	if dec != nil && dec.AbortRun {
		slog.Warn("Run aborted by extension", "event", string(ev), "reason", dec.AbortReason)
	}
	return dec
}

// resultMessages returns the message trace from a run result. Currently nil:
// fantasy.AgentResult exposes []fantasy.Message (per-step), but the extension
// Context.Messages is []message.Message (the domain type). These are distinct
// structs, not aliases, so flattening requires a conversion the evo loop does
// not need — extension consumers use Prompt + Response + ToolNames instead.
// When a future extension needs the full trace, add a fantasy->domain Message
// converter here.
func resultMessages(_ *fantasy.AgentResult) []message.Message {
	return nil
}

// resultResponseText extracts the agent's final textual response, nil-safe.
// Used to feed the evo lesson-distiller (prompt + response -> principle).
func resultResponseText(r *fantasy.AgentResult) string {
	if r == nil {
		return ""
	}
	return r.Response.Content.Text()
}

// resultToolNames returns the distinct tool names the agent called during the
// run, in first-use order. Each name is a skill the agent exercised — the /evo
// loop captures them as emergent skills for the fixed agent's manifest.
func resultToolNames(r *fantasy.AgentResult) []string {
	if r == nil {
		return nil
	}
	var names []string
	seen := make(map[string]bool)
	for _, step := range r.Steps {
		for _, msg := range step.Messages {
			for _, part := range msg.Content {
				if tc, ok := fantasy.AsMessagePart[fantasy.ToolCallPart](part); ok {
					name := strings.TrimSpace(tc.ToolName)
					if name == "" || seen[name] {
						continue
					}
					seen[name] = true
					names = append(names, name)
				}
			}
		}
	}
	return names
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
	agentPrompt, err := promptForAgent(agentCfg, promptOpts...)
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

// injectEvolutionContext loads unapplied evolution patches and appends them
// to the active agent's system prompt so the agent applies lessons learned.
func (c *coordinator) injectEvolutionContext(ctx context.Context) {
	patchDir := filepath.Join(c.cfg.WorkingDir(), ".mocode")
	store, err := evolution.NewPatchStore(patchDir)
	if err != nil {
		return // not fatal
	}
	loader := evolution.NewPatchLoader(store)
	ctxStr, err := loader.BuildContext()
	if err != nil || ctxStr == "" {
		return
	}
	c.currentAgent.SetSystemPrompt(c.currentAgent.SystemPrompt() + "\n\n<evolution_context>\n" + ctxStr + "\n</evolution_context>")
}

func (c *coordinator) Cancel(sessionID string) {
	c.currentAgent.Cancel(sessionID)
}

// CancelSubagent stops a single sub-agent dispatched by the Agent tool,
// identified by its user-visible subagentID (params.AgentID, e.g.
// "<parentToolCallID>-1"). The parent session is left running.
//
// The mapping from subagentID to internal sub-session ID is maintained by
// runSubAgentWithMeta and removed in a defer. If the subagentID is
// unknown (already completed, never registered, or never ran in this
// process) the call is a silent no-op — that matches the behaviour of
// sessionAgent.Cancel for arbitrary session IDs.
func (c *coordinator) CancelSubagent(subagentID string) {
	if subagentID == "" || c.subagentIndex == nil {
		return
	}
	subSessionID, ok := c.subagentIndex.Take(subagentID)
	if !ok || subSessionID == "" {
		return
	}
	c.currentAgent.Cancel(subSessionID)
}

func (c *coordinator) CancelAll() {
	c.currentAgent.CancelAll()
}

func (c *coordinator) ClearQueue(sessionID string) {
	c.currentAgent.ClearQueue(sessionID)
}

func (c *coordinator) IsBusy() bool {
	return c.currentAgent.IsBusy()
}

func (c *coordinator) IsSessionBusy(sessionID string) bool {
	return c.currentAgent.IsSessionBusy(sessionID)
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
		agentPrompt, err := promptForAgent(agentCfg, promptOpts...)
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

func (c *coordinator) QueuedPrompts(sessionID string) int {
	return c.currentAgent.QueuedPrompts(sessionID)
}

func (c *coordinator) QueuedPromptsList(sessionID string) []string {
	return c.currentAgent.QueuedPromptsList(sessionID)
}

func (c *coordinator) Summarize(ctx context.Context, sessionID string) error {
	providerCfg, ok := c.cfg.Config().Providers.Get(c.currentAgent.Model().ModelCfg.Provider)
	if !ok {
		return errModelProviderNotConfigured
	}
	_, err := c.currentAgent.Summarize(ctx, sessionID, getProviderOptions(c.currentAgent.Model(), providerCfg))
	return err
}

func (c *coordinator) isUnauthorized(err error) bool {
	var providerErr *fantasy.ProviderError
	return errors.As(err, &providerErr) && providerErr.StatusCode == http.StatusUnauthorized
}

func (c *coordinator) refreshOAuth2Token(ctx context.Context, providerCfg config.ProviderConfig) error {
	if err := c.cfg.RefreshOAuthToken(ctx, config.ScopeGlobal, providerCfg.ID); err != nil {
		slog.Error("Failed to refresh OAuth token after 401 error", "provider", providerCfg.ID, "error", err)
		return err
	}
	if err := c.UpdateModels(ctx); err != nil {
		return err
	}
	return nil
}

func (c *coordinator) refreshApiKeyTemplate(ctx context.Context, providerCfg config.ProviderConfig) error {
	newAPIKey, err := c.cfg.Resolve(providerCfg.APIKeyTemplate)
	if err != nil {
		slog.Error("Failed to re-resolve API key after 401 error", "provider", providerCfg.ID, "error", err)
		return err
	}

	providerCfg.APIKey = newAPIKey
	c.cfg.Config().Providers.Set(providerCfg.ID, providerCfg)

	if err := c.UpdateModels(ctx); err != nil {
		return err
	}
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

// runSubAgent runs a sub-agent and handles session management and cost accumulation.
// It creates a sub-session, runs the agent with the given prompt, and propagates
// the cost to the parent session.
func (c *coordinator) runSubAgent(ctx context.Context, params subAgentParams) (fantasy.ToolResponse, error) {
	resp, _, err := c.runSubAgentWithMeta(ctx, params)
	return resp, err
}

// runSubAgentWithMeta is the metadata-returning variant of runSubAgent. It is
// used by the parallel and DAG batch runners so they can populate
// TaskResult.DurationMs and TaskResult.Usage on each envelope.
func (c *coordinator) runSubAgentWithMeta(ctx context.Context, params subAgentParams) (fantasy.ToolResponse, subAgentResult, error) {
	startTime := time.Now()

	// Create sub-session
	agentToolSessionID := c.sessions.CreateAgentToolSessionID(params.AgentMessageID, params.ToolCallID)
	session, err := c.sessions.CreateTaskSession(ctx, agentToolSessionID, params.SessionID, params.SessionTitle)
	if err != nil {
		duration := time.Since(startTime)
		c.publishSubagentCompleted(ctx, params, notify.SubagentStatusError, duration, notify.SubagentTokenUsage{},
			"", fmt.Sprintf("create session: %s", err))
		return fantasy.ToolResponse{}, subAgentResult{DurationMs: duration.Milliseconds()}, fmt.Errorf("create session: %w", err)
	}

	// Call session setup function if provided
	if params.SessionSetup != nil {
		params.SessionSetup(session.ID)
	}

	// Get model configuration
	model := params.Agent.Model()
	maxTokens := model.CatwalkCfg.DefaultMaxTokens
	if model.ModelCfg.MaxTokens != 0 {
		maxTokens = model.ModelCfg.MaxTokens
	}

	providerCfg, ok := c.cfg.Config().Providers.Get(model.ModelCfg.Provider)
	if !ok {
		duration := time.Since(startTime)
		c.publishSubagentCompleted(ctx, params, notify.SubagentStatusError, duration, notify.SubagentTokenUsage{},
			"", errModelProviderNotConfigured.Error())
		return fantasy.ToolResponse{}, subAgentResult{DurationMs: duration.Milliseconds()}, errModelProviderNotConfigured
	}

	// Register the sub-agent in the index so an external caller can stop
	// it via CancelSubagent without cancelling the parent session. The
	// entry is removed when the agent run returns — whether the run
	// finishes, errors out, or the sub-agent is cancelled.
	if params.AgentID != "" && c.subagentIndex != nil {
		c.subagentIndex.Set(params.AgentID, agentToolSessionID)
		defer c.subagentIndex.Del(params.AgentID)
	}

	// Extension lifecycle for the sub-agent run, mirroring the main Run path
	// so the /evo observability loop sees delegated runs too.
	c.fireExtensions(ctx, extension.EventBeforeRun, &extension.Context{
		SessionID: session.ID, AgentName: params.AgentID, Prompt: params.Prompt,
	})

	// Run the agent
	result, err := params.Agent.Run(ctx, SessionAgentCall{
		SessionID:        session.ID,
		Prompt:           params.Prompt,
		MaxOutputTokens:  maxTokens,
		ProviderOptions:  getProviderOptions(model, providerCfg),
		Temperature:      model.ModelCfg.Temperature,
		TopP:             model.ModelCfg.TopP,
		TopK:             model.ModelCfg.TopK,
		FrequencyPenalty: model.ModelCfg.FrequencyPenalty,
		PresencePenalty:  model.ModelCfg.PresencePenalty,
		NonInteractive:   true,
	})
	duration := time.Since(startTime)

	// Extension lifecycle: AfterRun or OnError for the sub-agent.
	subEvt := extension.EventAfterRun
	if err != nil {
		subEvt = extension.EventOnError
	}
	c.fireExtensions(ctx, subEvt, &extension.Context{
		SessionID: session.ID, AgentName: params.AgentID, Prompt: params.Prompt,
		Err: err,
	})

	if err != nil {
		// Distinguish user-initiated cancellation from a real error so
		// the frontend can show the right terminal state. ctx.Err() is
		// non-nil whenever the sub-agent's own ctx was cancelled
		// (CancelSubagent) or its parent ctx was cancelled (parent
		// session cancelled). In either case the LLM client returns a
		// wrapped error and we want to label it cancelled, not error.
		status := notify.SubagentStatusError
		if ctxErr := ctx.Err(); ctxErr != nil {
			status = notify.SubagentStatusCancelled
		}
		slog.Warn(
			"Sub-agent failed",
			"sub_session", session.ID,
			"tool_call_id", params.ToolCallID,
			"duration_ms", duration.Milliseconds(),
			"status", string(status),
			"error", err,
		)
		c.publishSubagentCompleted(ctx, params, status, duration, notify.SubagentTokenUsage{},
			"", err.Error())
		return fantasy.NewTextErrorResponse(fmt.Sprintf("error generating response: %s", err)),
			subAgentResult{DurationMs: duration.Milliseconds()},
			nil
	}
	if result == nil {
		c.publishSubagentCompleted(ctx, params, notify.SubagentStatusError, duration, notify.SubagentTokenUsage{},
			"", "sub-agent returned no result (session busy)")
		return fantasy.NewTextErrorResponse("sub-agent returned no result (session busy)"),
			subAgentResult{DurationMs: duration.Milliseconds()},
			nil
	}

	meta := subAgentResult{
		DurationMs: duration.Milliseconds(),
		Usage:      result.TotalUsage,
	}

	// Update parent session cost
	if err := c.updateParentSessionCost(ctx, session.ID, params.SessionID); err != nil {
		// Best-effort: still publish a completed event so the UI can move
		// out of the running state, but include the cost update error in
		// the payload for observability.
		c.publishSubagentCompleted(ctx, params, notify.SubagentStatusError, duration, subagentUsageFromTotal(result.TotalUsage),
			firstLine(result.Response.Content.Text()), err.Error())
		return fantasy.ToolResponse{}, meta, err
	}

	c.publishSubagentCompleted(ctx, params, notify.SubagentStatusSuccess, duration, subagentUsageFromTotal(result.TotalUsage),
		firstLine(result.Response.Content.Text()), "")

	return fantasy.NewTextResponse(result.Response.Content.Text()), meta, nil
}

// publishSubagentCompleted emits a SubagentCompleted notification when the
// coordinator has a notify publisher configured. The call is a no-op when no
// publisher is wired (e.g. inside tests) so callers do not need to nil-check.
//
// The context argument is intentionally unused today: pubsub.Publish is
// non-blocking and discards events when no subscribers are attached, so
// cancellation should not gate emission. It is part of the signature so
// future refinements (e.g. honouring a parent ctx when bridging to a
// downstream that may block) can adopt it without churning call sites.
func (c *coordinator) publishSubagentCompleted(
	_ context.Context, // reserved for future cancellation propagation; see doc comment
	params subAgentParams,
	status notify.SubagentStatus,
	duration time.Duration,
	usage notify.SubagentTokenUsage,
	summary string,
	errMsg string,
) {
	if c.notify == nil {
		return
	}
	c.notify.Publish(pubsub.CreatedEvent, notify.Notification{
		SessionID: params.SessionID,
		Type:      notify.TypeSubagentCompleted,
		SubagentCompleted: &notify.SubagentCompletedEvent{
			ParentSessionID:  params.SessionID,
			ParentToolCallID: params.ToolCallID,
			AgentID:          params.AgentID,
			SubagentType:     params.SubagentType,
			Status:           status,
			DurationMs:       duration.Milliseconds(),
			Usage:            usage,
			Summary:          summary,
			Error:            errMsg,
		},
	})
}

// subagentUsageFromTotal converts a fantasy provider's TotalUsage into the
// notify package's sub-agent token usage representation.
func subagentUsageFromTotal(usage fantasy.Usage) notify.SubagentTokenUsage {
	return notify.SubagentTokenUsage{
		Input:         usage.InputTokens,
		Output:        usage.OutputTokens,
		CacheRead:     usage.CacheReadTokens,
		CacheCreation: usage.CacheCreationTokens,
		Total:         usage.TotalTokens,
	}
}

// updateParentSessionCost accumulates the cost from a child session to its parent session.
// Uses atomic increment to prevent lost updates when multiple sub-agents run concurrently.
func (c *coordinator) updateParentSessionCost(ctx context.Context, childSessionID, parentSessionID string) error {
	childSession, err := c.sessions.Get(ctx, childSessionID)
	if err != nil {
		return fmt.Errorf("get child session: %w", err)
	}

	if childSession.Cost == 0 {
		return nil // Nothing to add
	}

	// Use atomic increment to safely accumulate cost from concurrent sub-agents.
	// This prevents lost updates that would occur with read-modify-write.
	if err := c.sessions.IncrementCost(ctx, parentSessionID, childSession.Cost); err != nil {
		return fmt.Errorf("increment parent cost: %w", err)
	}

	return nil
}

// discoverSkills runs the skill discovery pipeline and returns both the
// pre-filter (all discovered, after dedup) and post-filter (active) lists.
// It also emits a single diagnostic log line summarising the outcome to
// help track skill-loading health over time.
func discoverSkills(cfg *config.ConfigStore) (allSkills, activeSkills []*skills.Skill) {
	builtin, builtinStates := skills.DiscoverBuiltinWithStates()
	discovered := append([]*skills.Skill(nil), builtin...)

	var userStates []*skills.SkillState
	var userPaths []string

	opts := cfg.Config().Options
	if opts != nil && len(opts.SkillsPaths) > 0 {
		userPaths = make([]string, 0, len(opts.SkillsPaths))
		for _, pth := range opts.SkillsPaths {
			expanded := infra.Long(pth)
			if strings.HasPrefix(expanded, "$") {
				if resolved, err := cfg.Resolver().ResolveValue(expanded); err == nil {
					expanded = resolved
				}
			}
			userPaths = append(userPaths, expanded)
		}
		var userSkills []*skills.Skill
		userSkills, userStates = skills.DiscoverWithStates(userPaths)
		discovered = append(discovered, userSkills...)
	}

	allSkills = skills.Deduplicate(discovered)
	var disabledSkills []string
	if opts != nil {
		disabledSkills = opts.DisabledSkills
	}
	activeSkills = skills.Filter(allSkills, disabledSkills)

	logDiscoveryStats(builtin, builtinStates, userStates, userPaths, allSkills, activeSkills, disabledSkills)
	return allSkills, activeSkills
}

// logTurnSkillUsage emits a per-turn diagnostic line showing which skills
// (if any) were loaded during this turn and which looked relevant based on
// a cheap keyword match against the user prompt. The goal is to surface
// "should-have-loaded but didn't" situations for later analysis.
//
// Logged at Info level under component=skills; heavy fields are elided when
// there is nothing interesting to report.
func logTurnSkillUsage(
	sessionID string,
	prompt string,
	activeSkills []*skills.Skill,
	tracker *skills.Tracker,
	before []string,
) {
	if tracker == nil || len(activeSkills) == 0 {
		return
	}

	after := tracker.LoadedNames()

	beforeSet := make(map[string]bool, len(before))
	for _, n := range before {
		beforeSet[n] = true
	}
	var loadedThisTurn []string
	for _, n := range after {
		if !beforeSet[n] {
			loadedThisTurn = append(loadedThisTurn, n)
		}
	}

	slog.Info(
		"Skill turn summary",
		"component", "skills",
		"session_id", sessionID,
		"prompt_len", len(prompt),
		"active_total", len(activeSkills),
		"loaded_total", len(after),
		"loaded_this_turn", loadedThisTurn,
	)
}

// logDiscoveryStats emits a single structured log line summarising skill
// discovery for the current session. It is intentionally low-volume: one
// line per session start.
func logDiscoveryStats(
	builtin []*skills.Skill,
	builtinStates, userStates []*skills.SkillState,
	userPaths []string,
	allSkills, activeSkills []*skills.Skill,
	disabled []string,
) {
	countErrors := func(states []*skills.SkillState) int {
		n := 0
		for _, s := range states {
			if s.State == skills.StateError {
				n++
			}
		}
		return n
	}

	userOK := 0
	for _, s := range userStates {
		if s.State == skills.StateNormal {
			userOK++
		}
	}

	activeNames := make([]string, 0, len(activeSkills))
	for _, s := range activeSkills {
		activeNames = append(activeNames, s.Name)
	}

	xml := skills.ToPromptXML(activeSkills)

	slog.Info(
		"Skill discovery complete",
		"component", "skills",
		"builtin_ok", len(builtin),
		"builtin_errors", countErrors(builtinStates),
		"user_ok", userOK,
		"user_errors", countErrors(userStates),
		"user_paths", len(userPaths),
		"deduped_total", len(allSkills),
		"active", len(activeSkills),
		"disabled", len(disabled),
		"prompt_bytes", len(xml),
		"prompt_tok_est", skills.ApproxTokenCount(xml),
		"active_names", activeNames,
	)
}

// getSessionContext returns a human-readable string describing the
// current session's branch/snapshot/revert state.
func getSessionContext(ctx context.Context, sessions session.Service, sessionID string) string {
	if sessionID == "" {
		return ""
	}
	sess, err := sessions.Get(ctx, sessionID)
	if err != nil {
		return ""
	}

	var parts []string
	if sess.ParentSessionID != "" {
		parts = append(parts, fmt.Sprintf("This session is a branch of %s.", sess.ParentSessionID[:8]))
	}
	if sess.RevertSessionID != "" {
		parts = append(parts, "Files have been reverted to a previous checkpoint.")
	}
	if sess.ActiveSnapshotID != "" {
		parts = append(parts, "File changes are being tracked with checkpoints.")
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ")
}
