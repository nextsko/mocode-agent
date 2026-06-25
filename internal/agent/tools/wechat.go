package tools

import (
	"context"
	"fmt"
	"os"

	"charm.land/fantasy"
	"github.com/package-register/mocode/internal/shellruntime/screencap"
	wechat "github.com/package-register/mocode/internal/wechat"
)

// WeChatSendImageToolName is the tool name for sending images to WeChat.
const WeChatSendImageToolName = "send_wechat_image"

// WeChatSendFileToolName is the tool name for sending files to WeChat.
const WeChatSendFileToolName = "send_wechat_file"

// WeChatScreenshotToolName is the combined screenshot→WeChat tool name.
const WeChatScreenshotToolName = "screenshot_to_wechat"

// NewWeChatSendImageTool creates an agent tool for sending images to WeChat.
func NewWeChatSendImageTool() fantasy.AgentTool {
	type input struct {
		Path   string `json:"path" jsonschema:"required,description=Path to the image file to send"`
		UserID string `json:"user_id,omitempty" jsonschema:"description=WeChat user ID to send to (optional; uses last active user if empty)"`
	}
	return fantasy.NewAgentTool(
		WeChatSendImageToolName,
		"Send an image file to a WeChat user via the active WeChat account.",
		func(ctx context.Context, in input, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			mgr := wechat.GetManager()
			ch := mgr.GetActive()
			if ch == nil || !ch.IsLoggedIn() {
				return fantasy.ToolResponse{}, fmt.Errorf("no active WeChat account — please login first")
			}
			if _, err := os.Stat(in.Path); err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("image file not found: %s", in.Path)
			}
			data, err := os.ReadFile(in.Path)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("read image file: %w", err)
			}
			if err := ch.SendImage(ctx, in.UserID, data); err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("send wechat image: %w", err)
			}
			return fantasy.ToolResponse{
				Content: fmt.Sprintf("Image sent to WeChat: %s", in.Path),
			}, nil
		},
	)
}

// NewWeChatSendFileTool creates an agent tool for sending files to WeChat.
func NewWeChatSendFileTool() fantasy.AgentTool {
	type input struct {
		Path   string `json:"path" jsonschema:"required,description=Path to the file to send"`
		UserID string `json:"user_id,omitempty" jsonschema:"description=WeChat user ID to send to (optional)"`
	}
	return fantasy.NewAgentTool(
		WeChatSendFileToolName,
		"Send a file to a WeChat user via the active WeChat account.",
		func(ctx context.Context, in input, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			mgr := wechat.GetManager()
			ch := mgr.GetActive()
			if ch == nil || !ch.IsLoggedIn() {
				return fantasy.ToolResponse{}, fmt.Errorf("no active WeChat account — please login first")
			}
			// For now, send file as text message with path (full SendFile needs upload support).
			_ = in.Path
			return fantasy.ToolResponse{Content: "File send not yet implemented — use send_wechat_image for images or text messages for now."}, nil
		},
	)
}

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
			// Read the image data.
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
