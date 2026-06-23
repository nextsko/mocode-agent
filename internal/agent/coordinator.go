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
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"github.com/package-register/mocode/internal/agent/notify"
	"github.com/package-register/mocode/internal/agent/prompt"
	"github.com/package-register/mocode/internal/agent/tools"
	"github.com/package-register/mocode/internal/config"
	"github.com/package-register/mocode/internal/csync"
	"github.com/package-register/mocode/internal/errcoll"
	"github.com/package-register/mocode/internal/evolution"
	"github.com/package-register/mocode/internal/filetracker"
	"github.com/package-register/mocode/internal/history"
	"github.com/package-register/mocode/internal/hooks"
	"github.com/package-register/mocode/internal/infra"
	"github.com/package-register/mocode/internal/knowledge/memory"
	"github.com/package-register/mocode/internal/log"
	"github.com/package-register/mocode/internal/lsp"
	"github.com/package-register/mocode/internal/orchestration"
	"github.com/package-register/mocode/internal/permission"
	"github.com/package-register/mocode/internal/pubsub"
	"github.com/package-register/mocode/internal/session"
	"github.com/package-register/mocode/internal/session/message"
	"github.com/package-register/mocode/internal/session/sessionlog"
	"github.com/package-register/mocode/internal/skills"
	"github.com/package-register/mocode/internal/store"
	"github.com/package-register/mocode/internal/tools/screencap"
	"golang.org/x/sync/errgroup"

	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/azure"
	"charm.land/fantasy/providers/bedrock"
	"charm.land/fantasy/providers/google"
	"charm.land/fantasy/providers/openai"
	"charm.land/fantasy/providers/openaicompat"
	"charm.land/fantasy/providers/openrouter"
	"charm.land/fantasy/providers/vercel"
	openaisdk "github.com/charmbracelet/openai-go/option"
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
	swarmWF        *orchestration.WorkflowRuntime // nil if swarm mode not active
	sessionLogDir  string                 // base dir for session logs
	errorLearner   *evolution.ErrorLearner
	errorCollector *errcoll.Collector
	sessionSearch  *store.SessionSearch
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
	result, originalErr := run()
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
		SessionSearch:   c.sessionSearch,
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
	allTools = append(allTools,
		tools.NewWeChatSendImageTool(),
		tools.NewWeChatSendFileTool(),
		tools.NewWeChatScreenshotTool(screenshotDir),
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

func (c *coordinator) buildAnthropicProvider(baseURL, apiKey string, headers map[string]string, providerID string) (fantasy.Provider, error) {
	var opts []anthropic.Option

	switch {
	case strings.HasPrefix(apiKey, "Bearer "):
		// NOTE: Prevent the SDK from picking up the API key from env.
		os.Setenv("ANTHROPIC_API_KEY", "")
		headers["Authorization"] = apiKey
	case providerID == string(catwalk.InferenceProviderMiniMax) || providerID == string(catwalk.InferenceProviderMiniMaxChina):
		// NOTE: Prevent the SDK from picking up the API key from env.
		os.Setenv("ANTHROPIC_API_KEY", "")
		headers["Authorization"] = "Bearer " + apiKey
	case apiKey != "":
		// X-Api-Key header
		opts = append(opts, anthropic.WithAPIKey(apiKey))
	}

	if len(headers) > 0 {
		opts = append(opts, anthropic.WithHeaders(headers))
	}

	if baseURL != "" {
		opts = append(opts, anthropic.WithBaseURL(baseURL))
	}

	if c.cfg.Config().Options.Debug {
		httpClient := log.NewHTTPClient()
		opts = append(opts, anthropic.WithHTTPClient(httpClient))
	}
	return anthropic.New(opts...)
}

func (c *coordinator) buildOpenaiProvider(baseURL, apiKey string, headers map[string]string) (fantasy.Provider, error) {
	opts := []openai.Option{
		openai.WithAPIKey(apiKey),
		openai.WithUseResponsesAPI(),
	}
	if c.cfg.Config().Options.Debug {
		httpClient := log.NewHTTPClient()
		opts = append(opts, openai.WithHTTPClient(httpClient))
	}
	if len(headers) > 0 {
		opts = append(opts, openai.WithHeaders(headers))
	}
	if baseURL != "" {
		opts = append(opts, openai.WithBaseURL(baseURL))
	}
	return openai.New(opts...)
}

func (c *coordinator) buildOpenrouterProvider(_, apiKey string, headers map[string]string) (fantasy.Provider, error) {
	opts := []openrouter.Option{
		openrouter.WithAPIKey(apiKey),
	}
	if c.cfg.Config().Options.Debug {
		httpClient := log.NewHTTPClient()
		opts = append(opts, openrouter.WithHTTPClient(httpClient))
	}
	if len(headers) > 0 {
		opts = append(opts, openrouter.WithHeaders(headers))
	}
	return openrouter.New(opts...)
}

func (c *coordinator) buildVercelProvider(_, apiKey string, headers map[string]string) (fantasy.Provider, error) {
	opts := []vercel.Option{
		vercel.WithAPIKey(apiKey),
	}
	if c.cfg.Config().Options.Debug {
		httpClient := log.NewHTTPClient()
		opts = append(opts, vercel.WithHTTPClient(httpClient))
	}
	if len(headers) > 0 {
		opts = append(opts, vercel.WithHeaders(headers))
	}
	return vercel.New(opts...)
}

func (c *coordinator) buildOpenaiCompatProvider(baseURL, apiKey string, headers map[string]string, extraBody map[string]any) (fantasy.Provider, error) {
	opts := []openaicompat.Option{
		openaicompat.WithBaseURL(baseURL),
		openaicompat.WithAPIKey(apiKey),
	}

	var httpClient *http.Client
	if c.cfg.Config().Options.Debug {
		httpClient = log.NewHTTPClient()
	}
	if httpClient != nil {
		opts = append(opts, openaicompat.WithHTTPClient(httpClient))
	}

	if len(headers) > 0 {
		opts = append(opts, openaicompat.WithHeaders(headers))
	}

	for extraKey, extraValue := range extraBody {
		opts = append(opts, openaicompat.WithSDKOptions(openaisdk.WithJSONSet(extraKey, extraValue)))
	}

	return openaicompat.New(opts...)
}

func (c *coordinator) buildAzureProvider(baseURL, apiKey string, headers map[string]string, options map[string]string) (fantasy.Provider, error) {
	opts := []azure.Option{
		azure.WithBaseURL(baseURL),
		azure.WithAPIKey(apiKey),
		azure.WithUseResponsesAPI(),
	}
	if c.cfg.Config().Options.Debug {
		httpClient := log.NewHTTPClient()
		opts = append(opts, azure.WithHTTPClient(httpClient))
	}
	if options == nil {
		options = make(map[string]string)
	}
	if apiVersion, ok := options["apiVersion"]; ok {
		opts = append(opts, azure.WithAPIVersion(apiVersion))
	}
	if len(headers) > 0 {
		opts = append(opts, azure.WithHeaders(headers))
	}

	return azure.New(opts...)
}

func (c *coordinator) buildBedrockProvider(apiKey string, headers map[string]string) (fantasy.Provider, error) {
	var opts []bedrock.Option
	if c.cfg.Config().Options.Debug {
		httpClient := log.NewHTTPClient()
		opts = append(opts, bedrock.WithHTTPClient(httpClient))
	}
	if len(headers) > 0 {
		opts = append(opts, bedrock.WithHeaders(headers))
	}
	switch {
	case apiKey != "":
		opts = append(opts, bedrock.WithAPIKey(apiKey))
	case os.Getenv("AWS_BEARER_TOKEN_BEDROCK") != "":
		opts = append(opts, bedrock.WithAPIKey(os.Getenv("AWS_BEARER_TOKEN_BEDROCK")))
	default:
		// Skip, let the SDK do authentication.
	}
	return bedrock.New(opts...)
}

func (c *coordinator) buildGoogleProvider(baseURL, apiKey string, headers map[string]string) (fantasy.Provider, error) {
	opts := []google.Option{
		google.WithBaseURL(baseURL),
		google.WithGeminiAPIKey(apiKey),
	}
	if c.cfg.Config().Options.Debug {
		httpClient := log.NewHTTPClient()
		opts = append(opts, google.WithHTTPClient(httpClient))
	}
	if len(headers) > 0 {
		opts = append(opts, google.WithHeaders(headers))
	}
	return google.New(opts...)
}

func (c *coordinator) buildGoogleVertexProvider(headers map[string]string, options map[string]string) (fantasy.Provider, error) {
	opts := []google.Option{}
	if c.cfg.Config().Options.Debug {
		httpClient := log.NewHTTPClient()
		opts = append(opts, google.WithHTTPClient(httpClient))
	}
	if len(headers) > 0 {
		opts = append(opts, google.WithHeaders(headers))
	}

	project := options["project"]
	location := options["location"]

	opts = append(opts, google.WithVertex(project, location))

	return google.New(opts...)
}

func (c *coordinator) isAnthropicThinking(model config.SelectedModel) bool {
	if model.Think {
		return true
	}
	opts, err := anthropic.ParseOptions(model.ProviderOptions)
	return err == nil && opts.Thinking != nil
}

func (c *coordinator) buildProvider(providerCfg config.ProviderConfig, model config.SelectedModel, isSubAgent bool) (fantasy.Provider, error) {
	headers := maps.Clone(providerCfg.ExtraHeaders)
	if headers == nil {
		headers = make(map[string]string)
	}

	// handle special headers for anthropic
	if providerCfg.Type == anthropic.Name && c.isAnthropicThinking(model) {
		if v, ok := headers["anthropic-beta"]; ok {
			headers["anthropic-beta"] = v + ",interleaved-thinking-2025-05-14"
		} else {
			headers["anthropic-beta"] = "interleaved-thinking-2025-05-14"
		}
	}

	apiKey, _ := c.cfg.Resolve(providerCfg.APIKey)
	baseURL, _ := c.cfg.Resolve(providerCfg.BaseURL)

	switch providerCfg.Type {
	case openai.Name:
		return c.buildOpenaiProvider(baseURL, apiKey, headers)
	case anthropic.Name:
		return c.buildAnthropicProvider(baseURL, apiKey, headers, providerCfg.ID)
	case openrouter.Name:
		return c.buildOpenrouterProvider(baseURL, apiKey, headers)
	case vercel.Name:
		return c.buildVercelProvider(baseURL, apiKey, headers)
	case azure.Name:
		return c.buildAzureProvider(baseURL, apiKey, headers, providerCfg.ExtraParams)
	case bedrock.Name:
		return c.buildBedrockProvider(apiKey, headers)
	case google.Name:
		return c.buildGoogleProvider(baseURL, apiKey, headers)
	case "google-vertex":
		return c.buildGoogleVertexProvider(headers, providerCfg.ExtraParams)
	case openaicompat.Name:
		switch providerCfg.ID {
		case string(catwalk.InferenceProviderZAI):
			if providerCfg.ExtraBody == nil {
				providerCfg.ExtraBody = map[string]any{}
			}
			providerCfg.ExtraBody["tool_stream"] = true
		}
		return c.buildOpenaiCompatProvider(baseURL, apiKey, headers, providerCfg.ExtraBody)
	default:
		return nil, fmt.Errorf("provider type not supported: %q", providerCfg.Type)
	}
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

// SetMainAgent switches the active agent to the given agent/mode ID.
// It builds the target agent's system prompt and tools asynchronously
// via readyWg so that the next Run() call waits for them to be ready.
// When a swarm workflow is active, it records a handoff in the audit trail.
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

	// Record handoff in swarm workflow if active.
	if c.swarmWF != nil {
		c.swarmWF.RecordHandoff(fromAgent, agentID, c.swarmWF.ActiveTaskID, "")
		c.swarmWF.SetActive(c.swarmWF.ActiveTaskID, agentID)
	}

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
		slog.Warn("Sub-agent failed",
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

	slog.Info("Skill turn summary",
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

	slog.Info("Skill discovery complete",
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
