package coordinator

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"charm.land/fantasy"

	"github.com/nextsko/mocode-agent/internal/core/agent/agenttooladapter"
	"github.com/nextsko/mocode-agent/internal/core/agent/lifecycle"
	"github.com/nextsko/mocode-agent/internal/core/agent/prompt"
	"github.com/nextsko/mocode-agent/internal/core/permission"
	"github.com/nextsko/mocode-agent/internal/core/tools"
)

//go:embed templates/agentic_fetch.md
var agenticFetchToolDescription []byte

// agenticFetchValidationResult holds the validated parameters from the tool call context.
type agenticFetchValidationResult struct {
	SessionID      string
	AgentMessageID string
}

// validateAgenticFetchParams validates the tool call parameters and extracts required context values.
func validateAgenticFetchParams(ctx context.Context, params tools.AgenticFetchParams) (agenticFetchValidationResult, error) {
	if params.Prompt == "" {
		return agenticFetchValidationResult{}, errors.New("prompt is required")
	}

	sessionID := tools.GetSessionFromContext(ctx)
	if sessionID == "" {
		return agenticFetchValidationResult{}, errors.New("session id missing from context")
	}

	agentMessageID := tools.GetMessageFromContext(ctx)
	if agentMessageID == "" {
		return agenticFetchValidationResult{}, errors.New("agent message id missing from context")
	}

	return agenticFetchValidationResult{
		SessionID:      sessionID,
		AgentMessageID: agentMessageID,
	}, nil
}

//go:embed templates/agentic_fetch_prompt.md.tpl
var agenticFetchPromptTmpl []byte

func (c *Coordinator) agenticFetchTool(_ context.Context, client *http.Client) fantasy.AgentTool {
	if client == nil {
		client = c.cfg.Config().HTTPClient(c.cfg.Resolver(), 30*time.Second)
	}

	return fantasy.NewParallelAgentTool(
		tools.AgenticFetchToolName,
		tools.FirstLineDescription(agenticFetchToolDescription),
		func(ctx context.Context, params tools.AgenticFetchParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			validationResult, err := validateAgenticFetchParams(ctx, params)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}

			// Determine description based on mode.
			var description string
			if params.URL != "" {
				description = fmt.Sprintf("Fetch and analyze content from URL: %s", params.URL)
			} else {
				description = "Search the web and analyze results"
			}

			p, err := c.permissions.Request(ctx,
				permission.CreatePermissionRequest{
					SessionID:   validationResult.SessionID,
					Path:        c.cfg.WorkingDir(),
					ToolCallID:  call.ID,
					ToolName:    tools.AgenticFetchToolName,
					Action:      "fetch",
					Description: description,
					Params:      tools.AgenticFetchPermissionsParams(params),
				},
			)
			if err != nil {
				return fantasy.ToolResponse{}, err
			}
			if !p {
				return tools.NewPermissionDeniedResponse(), nil
			}

			tmpDir, err := os.MkdirTemp(os.TempDir(), "Mocode-fetch-*")
			if err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("Failed to create temporary directory: %s", err)), nil
			}
			defer func() { _ = os.RemoveAll(tmpDir) }()

			fullPrompt, err := buildAgenticFetchPrompt(ctx, client, tmpDir, params)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}

			promptOpts := []prompt.Option{
				prompt.WithWorkingDir(tmpDir),
			}

			promptTemplate, err := prompt.NewPrompt("agentic_fetch", string(agenticFetchPromptTmpl), promptOpts...)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("error creating prompt: %s", err)
			}

			_, small, err := c.buildAgentModels(ctx)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("error building models: %s", err)
			}

			systemPrompt, err := promptTemplate.Build(ctx, small.Model.Provider(), small.Model.Model(), c.cfg)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("error building system prompt: %s", err)
			}

			smallProviderCfg, ok := c.cfg.Config().Providers.Get(small.ModelCfg.Provider)
			if !ok {
				return fantasy.ToolResponse{}, errors.New("small model provider not configured")
			}

			webFetchTool := agenttooladapter.New(tools.NewWebFetchPortTool(tmpDir, client), nil)
			webSearchTool := agenttooladapter.New(tools.NewWebSearchPortTool(client), nil)
			fetchTools := []fantasy.AgentTool{
				webFetchTool,
				webSearchTool,
				agenttooladapter.New(tools.NewGlobPortTool(tmpDir), nil),
				agenttooladapter.New(tools.NewGrepPortTool(tmpDir, c.cfg.Config().Tools.Grep), nil),
				agenttooladapter.New(tools.NewSourcegraphPortTool(client), nil),
				// The sub-agent reads fetched pages from a temp directory, so it
				// has no use for the internal resource namespaces.
				agenttooladapter.New(tools.NewViewPortTool(c.lspManager, c.permissions, c.filetracker, nil, nil, tmpDir), nil),
			}

			// Sub-agent tools run without hook interception. The top-level
			// `agentic_fetch` call itself is already wrapped from the coder's
			// side; firing hooks again for every inner tool call would run
			// the user's hooks N times per delegated turn.

			agent := lifecycle.NewSessionAgent(lifecycle.SessionAgentOptions{
				LargeModel:           small, // Use small model for both (fetch doesn't need large)
				SmallModel:           small,
				SystemPromptPrefix:   smallProviderCfg.SystemPromptPrefix,
				SystemPrompt:         systemPrompt,
				DisableAutoSummarize: c.cfg.Config().Options.DisableAutoSummarize,
				IsYolo:               c.permissions.SkipRequests(),
				Sessions:             c.sessions,
				Messages:             c.messages,
				Tools:                fetchTools,
			})

			return c.runSubAgent(ctx, subAgentParams{
				Agent:          agent,
				SessionID:      validationResult.SessionID,
				AgentMessageID: validationResult.AgentMessageID,
				ToolCallID:     call.ID,
				Prompt:         fullPrompt,
				SessionTitle:   "Fetch Analysis",
				SessionSetup: func(sessionID string) {
					c.permissions.AutoApproveSession(sessionID)
				},
			})
		})
}

// buildAgenticFetchPrompt builds the sub-agent prompt for the agentic fetch
// tool, fetching the target URL first when one is given.
func buildAgenticFetchPrompt(ctx context.Context, client *http.Client, tmpDir string, params tools.AgenticFetchParams) (string, error) {
	if params.URL == "" {
		// Search mode: let the sub-agent search and fetch as needed.
		return fmt.Sprintf("%s\n\nUse the web_search tool to find relevant information. Break down the question into smaller, focused searches if needed. After searching, use web_fetch to get detailed content from the most relevant results.", params.Prompt), nil
	}

	// URL mode: fetch the URL content first.
	content, err := tools.FetchURLAndConvert(ctx, client, params.URL)
	if err != nil {
		return "", fmt.Errorf("failed to fetch URL: %s", err)
	}

	if len(content) <= tools.LargeContentThreshold {
		return fmt.Sprintf("%s\n\nWeb page URL: %s\n\n<webpage_content>\n%s\n</webpage_content>", params.Prompt, params.URL, content), nil
	}

	tempFilePath, err := writeTempPage(tmpDir, content)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s\n\nThe web page from %s has been saved to: %s\n\nUse the view and grep tools to analyze this file and extract the requested information.", params.Prompt, params.URL, tempFilePath), nil
}

// writeTempPage writes fetched page content to a temporary markdown file in dir
// and returns its path.
func writeTempPage(dir, content string) (string, error) {
	tempFile, err := os.CreateTemp(dir, "page-*.md")
	if err != nil {
		return "", fmt.Errorf("failed to create temporary file: %s", err)
	}
	tempFilePath := tempFile.Name()

	if _, err := tempFile.WriteString(content); err != nil {
		tempFile.Close()
		return "", fmt.Errorf("failed to write content to file: %s", err)
	}
	tempFile.Close()
	return tempFilePath, nil
}
