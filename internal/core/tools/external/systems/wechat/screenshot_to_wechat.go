package wechat

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/nextsko/mocode-agent/internal/core/shellruntime/screencap"
	"github.com/nextsko/mocode-agent/internal/core/tools/contract"
	"github.com/nextsko/mocode-agent/internal/domain/messenger"
)

// WeChatScreenshotToolName is the combined screenshot→WeChat tool name.
const WeChatScreenshotToolName = "screenshot_to_wechat"

// ScreenshotToWeChatParams holds the inputs for screenshot_to_wechat.
type ScreenshotToWeChatParams struct {
	UserID string `json:"user_id,omitempty" description:"WeChat user ID to send to (optional)"`
}

// NewWeChatScreenshotPortTool is the framework-agnostic form of the combined
// screenshot→WeChat tool.  The messenger port resolves the active account; pass
// messenger.NoopMessenger{} when no integration is wired in.
func NewWeChatScreenshotPortTool(m messenger.Messenger, outputDir string) contract.Tool {
	return contract.NewFunc(
		WeChatScreenshotToolName,
		"Capture a screenshot and send it to a WeChat user via the active WeChat account.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"user_id": map[string]any{
					"type":        "string",
					"description": "WeChat user ID to send to (optional)",
				},
			},
		},
		func(ctx context.Context, _ contract.ToolContext, args json.RawMessage) (contract.ToolResult, error) {
			var params ScreenshotToWeChatParams
			if len(args) > 0 {
				if err := json.Unmarshal(args, &params); err != nil {
					return contract.ToolResult{}, fmt.Errorf("invalid parameters: %w", err)
				}
			}
			return runScreenshotToWeChat(ctx, m, outputDir, params)
		},
	)
}

// runScreenshotToWeChat captures the screen and sends the PNG to WeChat.
func runScreenshotToWeChat(
	ctx context.Context,
	m messenger.Messenger,
	outputDir string,
	params ScreenshotToWeChatParams,
) (contract.ToolResult, error) {
	sender := m.ActiveSender()
	if sender == nil || !sender.IsLoggedIn() {
		return contract.ToolResult{}, fmt.Errorf("no active WeChat account - please login first")
	}
	path, err := screencap.CapturePNG(outputDir)
	if err != nil {
		return contract.ToolResult{}, fmt.Errorf("capture screenshot: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return contract.ToolResult{}, fmt.Errorf("read screenshot: %w", err)
	}
	if err := sender.SendImage(ctx, params.UserID, data); err != nil {
		return contract.ToolResult{}, fmt.Errorf("send to WeChat: %w", err)
	}
	return contract.Text(fmt.Sprintf("Screenshot captured and sent to WeChat: %s", path)), nil
}
