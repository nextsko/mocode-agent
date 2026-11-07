package screencap

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nextsko/mocode-agent/internal/core/tools/contract"
)

// ScreenshotToolName is the registered tool name.
const ScreenshotToolName = "screenshot"

// NewTool creates the framework-agnostic screenshot tool.
func NewTool(outputDir string) contract.Tool {
	return contract.NewFunc(
		ScreenshotToolName,
		"Capture a screenshot of the current screen and save as PNG. Use when asked to see what's on screen.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"reason": map[string]any{
					"type":        "string",
					"description": "Brief reason for taking the screenshot.",
				},
			},
		},
		func(_ context.Context, _ contract.ToolContext, args json.RawMessage) (contract.ToolResult, error) {
			var in struct {
				Reason string `json:"reason"`
			}
			if len(args) > 0 {
				if err := json.Unmarshal(args, &in); err != nil {
					return contract.ToolResult{}, fmt.Errorf("invalid parameters: %w", err)
				}
			}
			path, err := CapturePNG(outputDir)
			if err != nil {
				return contract.ToolResult{}, fmt.Errorf("screenshot: %w", err)
			}
			return contract.Text(fmt.Sprintf("Screenshot saved: %s", path)), nil
		})
}
