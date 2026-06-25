package agent

import (
	"context"

	"charm.land/fantasy"
)

// AgentCallbacks holds lifecycle hooks for agent execution.
// All callbacks are optional. A nil callback is a no-op.
type AgentCallbacks struct {
	// BeforeAgent is called before the agent runs.
	// If it returns a non-nil response, the agent execution is skipped
	// and the response is returned directly.
	BeforeAgent func(ctx context.Context, call *SessionAgentCall) (*fantasy.AgentResult, error)

	// AfterAgent is called after the agent runs.
	// It receives the result and any error from execution.
	// If it returns a non-nil response, it replaces the original result.
	AfterAgent func(ctx context.Context, call *SessionAgentCall, result *fantasy.AgentResult, err error) (*fantasy.AgentResult, error)

	// BeforeModel is called before the LLM is invoked.
	// It can modify the system prompt or provider options.
	BeforeModel func(ctx context.Context, call *SessionAgentCall, systemPrompt *string) error

	// AfterTool is called after a tool execution returns.
	// It can modify the tool response before it's sent back to the LLM.
	AfterTool func(ctx context.Context, toolName string, response fantasy.ToolResponse) fantasy.ToolResponse
}

// RunBeforeAgent executes the BeforeAgent callback if set.
// Returns (result, nil) to skip agent execution, or (nil, nil) to continue.
func (c *AgentCallbacks) RunBeforeAgent(ctx context.Context, call *SessionAgentCall) (*fantasy.AgentResult, error) {
	if c == nil || c.BeforeAgent == nil {
		return nil, nil
	}
	return c.BeforeAgent(ctx, call)
}

// RunAfterAgent executes the AfterAgent callback if set.
func (c *AgentCallbacks) RunAfterAgent(ctx context.Context, call *SessionAgentCall, result *fantasy.AgentResult, err error) (*fantasy.AgentResult, error) {
	if c == nil || c.AfterAgent == nil {
		return result, err
	}
	return c.AfterAgent(ctx, call, result, err)
}

// RunBeforeModel executes the BeforeModel callback if set.
func (c *AgentCallbacks) RunBeforeModel(ctx context.Context, call *SessionAgentCall, systemPrompt *string) error {
	if c == nil || c.BeforeModel == nil {
		return nil
	}
	return c.BeforeModel(ctx, call, systemPrompt)
}

// RunAfterTool executes the AfterTool callback if set.
func (c *AgentCallbacks) RunAfterTool(ctx context.Context, toolName string, response fantasy.ToolResponse) fantasy.ToolResponse {
	if c == nil || c.AfterTool == nil {
		return response
	}
	return c.AfterTool(ctx, toolName, response)
}
