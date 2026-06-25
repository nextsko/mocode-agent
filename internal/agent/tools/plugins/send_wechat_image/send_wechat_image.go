package send_wechat_image

import (
	"context"
	"fmt"
	"os"

	"charm.land/fantasy"
	wechat "github.com/package-register/mocode/internal/wechat"
)

// WeChatSendImageToolName is the tool name for sending images to WeChat.
const WeChatSendImageToolName = "send_wechat_image"

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
