package agent

import (
	"context"
	_ "embed"

	"github.com/package-register/mocode/internal/core/agent/prompt"
	"github.com/package-register/mocode/internal/core/config"
)

//go:embed templates/coder.md.tpl
var coderPromptTmpl []byte

//go:embed templates/task.md.tpl
var taskPromptTmpl []byte

//go:embed templates/initialize.md.tpl
var initializePromptTmpl []byte

func coderPrompt(opts ...prompt.Option) (*prompt.Prompt, error) {
	systemPrompt, err := prompt.NewPrompt("coder", string(coderPromptTmpl), opts...)
	if err != nil {
		return nil, err
	}
	return systemPrompt, nil
}

func taskPrompt(opts ...prompt.Option) (*prompt.Prompt, error) {
	systemPrompt, err := prompt.NewPrompt("task", string(taskPromptTmpl), opts...)
	if err != nil {
		return nil, err
	}
	return systemPrompt, nil
}

func InitializePrompt(cfg *config.ConfigStore) (string, error) {
	systemPrompt, err := prompt.NewPrompt("initialize", string(initializePromptTmpl))
	if err != nil {
		return "", err
	}
	return systemPrompt.Build(context.Background(), "", "", cfg)
}

// rawPrompt creates a Prompt from a raw string (no template rendering).
// Used for custom agents whose prompt comes from .md files.
func rawPrompt(content string, opts ...prompt.Option) (*prompt.Prompt, error) {
	allOpts := append([]prompt.Option{prompt.WithRaw()}, opts...)
	return prompt.NewPrompt("custom", content, allOpts...)
}

// promptForAgent returns the appropriate Prompt for the given agent config.
// If the agent has a custom SystemPrompt, it uses that directly.
// Otherwise, it falls back to the coder or task template.
func promptForAgent(agentCfg config.Agent, opts ...prompt.Option) (*prompt.Prompt, error) {
	if agentCfg.SystemPrompt != "" {
		return rawPrompt(agentCfg.SystemPrompt, opts...)
	}
	switch agentCfg.ID {
	case config.AgentTask:
		return taskPrompt(opts...)
	default:
		return coderPrompt(opts...)
	}
}
