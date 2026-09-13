package prompt

import (
	"context"
	_ "embed"

	"github.com/nextsko/mocode-agent/internal/core/config"
)

//go:embed templates/coder.md.tpl
var coderPromptTmpl []byte

//go:embed templates/task.md.tpl
var taskPromptTmpl []byte

//go:embed templates/initialize.md.tpl
var initializePromptTmpl []byte

func coderPrompt(opts ...Option) (*Prompt, error) {
	systemPrompt, err := NewPrompt("coder", string(coderPromptTmpl), opts...)
	if err != nil {
		return nil, err
	}
	return systemPrompt, nil
}

func taskPrompt(opts ...Option) (*Prompt, error) {
	systemPrompt, err := NewPrompt("task", string(taskPromptTmpl), opts...)
	if err != nil {
		return nil, err
	}
	return systemPrompt, nil
}

func InitializePrompt(cfg *config.ConfigStore) (string, error) {
	systemPrompt, err := NewPrompt("initialize", string(initializePromptTmpl))
	if err != nil {
		return "", err
	}
	return systemPrompt.Build(context.Background(), "", "", cfg)
}

// rawPrompt creates a Prompt from a raw string (no template rendering).
// Used for custom agents whose prompt comes from .md files.
func rawPrompt(content string, opts ...Option) (*Prompt, error) {
	allOpts := append([]Option{WithRaw()}, opts...)
	return NewPrompt("custom", content, allOpts...)
}

// PromptForAgent returns the appropriate Prompt for the given agent config.
// If the agent has a custom SystemPrompt, it uses that directly.
// Otherwise, it falls back to the coder or task template.
func PromptForAgent(agentCfg config.Agent, opts ...Option) (*Prompt, error) {
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
