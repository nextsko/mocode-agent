package tools

import (
	"context"
	"fmt"
	"os"

	"charm.land/fantasy"

	"github.com/nextsko/mocode-agent/internal/core/shellruntime/screencap"
	"github.com/nextsko/mocode-agent/internal/domain/messenger"
)

// WeChatScreenshotToolName is the combined screenshot→WeChat tool name.
const WeChatScreenshotToolName = "screenshot_to_wechat"

// NewWeChatScreenshotTool creates a combined screenshot→WeChat tool.
// The messenger port resolves the active account; pass messenger.NoopMessenger{}
// when no integration is wired in.
func NewWeChatScreenshotTool(m messenger.Messenger, outputDir string) fantasy.AgentTool {
	type input struct {
		UserID string `json:"user_id,omitempty" jsonschema:"description=WeChat user ID to send to (optional)"`
	}
	return fantasy.NewAgentTool(
		WeChatScreenshotToolName,
		"Capture a screenshot and send it to a WeChat user via the active WeChat account.",
		func(ctx context.Context, in input, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			sender := m.ActiveSender()
			if sender == nil || !sender.IsLoggedIn() {
				return fantasy.ToolResponse{}, fmt.Errorf("no active WeChat account - please login first")
			}
			path, err := screencap.CapturePNG(outputDir)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("capture screenshot: %w", err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("read screenshot: %w", err)
			}
			if err := sender.SendImage(ctx, in.UserID, data); err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("send to WeChat: %w", err)
			}
			return fantasy.ToolResponse{
				Content: fmt.Sprintf("Screenshot captured and sent to WeChat: %s", path),
			}, nil
		},
	)
}
