// Package think provides a scratchpad tool for agent reasoning.
package think

import (
	"context"
	_ "embed"

	"charm.land/fantasy"

	"github.com/package-register/mocode/internal/core/agent/toolutil"
)

//go:embed think.md
var thinkDescription []byte

// ThinkToolName is the name of the think tool.
const ThinkToolName = "think"

// ThinkParams holds the parameters for a think tool call.
type ThinkParams struct {
	Thought string `json:"thought" description:"The reasoning or analysis to record before acting"`
}

// ThinkResponseMetadata holds metadata returned with a think tool response.
type ThinkResponseMetadata struct {
	Thought string `json:"thought"`
}

// NewThinkTool creates a stateless scratchpad tool that lets the agent record
// reasoning steps before taking any real action. It never retrieves external
// data or mutates state; it simply echoes the thought back so the model can
// reference it in subsequent turns.
func NewThinkTool() fantasy.AgentTool {
	return fantasy.NewAgentTool(
		ThinkToolName,
		toolutil.FirstLineDescription(thinkDescription),
		func(_ context.Context, params ThinkParams, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			meta := ThinkResponseMetadata{Thought: params.Thought}
			return fantasy.WithResponseMetadata(
				fantasy.NewTextResponse(params.Thought),
				meta,
			), nil
		},
	)
}
