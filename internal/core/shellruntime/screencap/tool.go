package screencap

import (
	"context"
	"fmt"

	"charm.land/fantasy"
)

// NewAgentTool creates a screenshot tool for the agent.
func NewAgentTool(outputDir string) fantasy.AgentTool {
	type input struct {
		Reason string `json:"reason" jsonschema:"description=Brief reason for taking the screenshot."`
	}
	return fantasy.NewAgentTool(
		"screenshot",
		"Capture a screenshot of the current screen and save as PNG. Use when asked to see what's on screen.",
		func(ctx context.Context, in input, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			path, err := CapturePNG(outputDir)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("screenshot: %w", err)
			}
			return fantasy.ToolResponse{
				Content: fmt.Sprintf("Screenshot saved: %s", path),
			}, nil
		},
	)
}
