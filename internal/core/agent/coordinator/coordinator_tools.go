package coordinator

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/azure"
	"charm.land/fantasy/providers/google"
	"charm.land/fantasy/providers/openai"
	"charm.land/fantasy/providers/openaicompat"
	"charm.land/fantasy/providers/openrouter"
	"charm.land/fantasy/providers/vercel"
	"github.com/nextsko/mocode-agent/internal/core/agent/failover"
	"github.com/nextsko/mocode-agent/internal/core/agent/lifecycle"
	"github.com/nextsko/mocode-agent/internal/core/agent/prompt"
	"github.com/nextsko/mocode-agent/internal/core/config"
	"github.com/nextsko/mocode-agent/internal/core/shellruntime/screencap"
	"github.com/nextsko/mocode-agent/internal/core/tools"
	"github.com/nextsko/mocode-agent/internal/domain/messenger"
	"github.com/nextsko/mocode-agent/internal/util/infra"
	"github.com/qjebbs/go-jsons"
)

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

func (c *Coordinator) buildAgent(ctx context.Context, prompt *prompt.Prompt, agent config.Agent, isSubAgent bool) (SessionAgent, error) {
	large, small, err := c.buildAgentModels(ctx, isSubAgent)
	if err != nil {
		return nil, err
	}

	largeProviderCfg, _ := c.cfg.Config().Providers.Get(large.ModelCfg.Provider)
	result := lifecycle.NewSessionAgent(lifecycle.SessionAgentOptions{
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
		WorkingDir:           c.cfg.WorkingDir(),
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

func (c *Coordinator) buildTools(ctx context.Context, agentCfg config.Agent, isSubAgent bool) ([]fantasy.AgentTool, error) {
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
		Questions:    c.questions,
		LSPManager:   c.lspManager,
		History:      c.history,
		FileTracker:  c.filetracker,
		Sessions:     c.sessions,
		Messages:     c.messages,
		AllSkills:    c.allSkills,
		ActiveSkills: c.activeSkills,
		SkillTracker: c.skillTracker,
		ModelName:    modelName,
		SummarySchedule: func(ctx context.Context, sessionID string) error {
			c.summaryQueue.Add(sessionID)
			return nil
		},
		SessionSearch: c.sessionSearch,
		HTTP:          c.httpFactory,
	}
	allTools = append(allTools, c.toolRegistry.Build(ctx, deps)...)

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
	screenshotDir := infra.ScreenshotsDir()
	allTools = append(allTools, screencap.NewAgentTool(screenshotDir))

	// ── Messaging tools (coordinator-owned, always available regardless of AllowedTools) ─
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

	// ── filter by AllowedTools ────────────────────────────────────────────────
	// coordinator-owned tools (agent, agentic_fetch, transfer_to_agent) bypass
	// the AllowedTools filter because they are already conditioned at the top.
	coordOwned := map[string]bool{
		AgentToolName:                  true,
		tools.AgenticFetchToolName:     true,
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
	return filteredTools, nil
}

// TODO: when we support multiple agents we need to change this so that we pass in the agent specific model config
func (c *Coordinator) buildAgentModels(ctx context.Context, isSubAgent bool) (Model, Model, error) {
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
func (c *Coordinator) withFailover(ctx context.Context, primary fantasy.LanguageModel, cfg config.SelectedModel) (fantasy.LanguageModel, error) {
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
