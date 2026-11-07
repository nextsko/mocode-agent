// Package think provides a scratchpad tool for agent reasoning.
package agenttools

import (
	_ "embed"
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
