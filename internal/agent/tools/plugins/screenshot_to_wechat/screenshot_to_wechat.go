package screenshot_to_wechat

import (
	"context"
	"fmt"
	"os"

	"charm.land/fantasy"
	"github.com/package-register/mocode/internal/shellruntime/screencap"
	wechat "github.com/package-register/mocode/internal/wechat"
)

// WeChatScreenshotToolName is the combined screenshot→WeChat tool name.
const WeChatScreenshotToolName = "screenshot_to_wechat"

// NewWeChatScreenshotTool creates a combined screenshot→WeChat tool.
func NewWeChatScreenshotTool(outputDir string) fantasy.AgentTool {
	type input struct {
		UserID string `json:"user_id,omitempty" jsonschema:"description=WeChat user ID to send to (optional)"`
	}
	return fantasy.NewAgentTool(
		WeChatScreenshotToolName,
		"Capture a screenshot and send it to a WeChat user via the active WeChat account.",
		func(ctx context.Context, in input, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			mgr := wechat.GetManager()
			ch := mgr.GetActive()
			if ch == nil || !ch.IsLoggedIn() {
				return fantasy.ToolResponse{}, fmt.Errorf("no active WeChat account — please login first")
			}
			path, err := screencap.CapturePNG(outputDir)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("capture screenshot: %w", err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("read screenshot: %w", err)
			}
			if err := ch.SendImage(ctx, in.UserID, data); err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("send to WeChat: %w", err)
			}
			return fantasy.ToolResponse{
				Content: fmt.Sprintf("Screenshot captured and sent to WeChat: %s", path),
			}, nil
		},
	)
}
