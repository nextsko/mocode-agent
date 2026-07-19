package tools

import (
	"context"
	"fmt"

	"charm.land/fantasy"

	"github.com/nextsko/mocode-agent/internal/domain/messenger"
)

// WeChatSendFileToolName is the tool name for sending files to WeChat.
const WeChatSendFileToolName = "send_wechat_file"

// NewWeChatSendFileTool creates an agent tool for sending files to WeChat.
// The messenger port resolves the active account; pass messenger.NoopMessenger{}
// when no integration is wired in.
func NewWeChatSendFileTool(m messenger.Messenger) fantasy.AgentTool {
	type input struct {
		Path   string `json:"path" jsonschema:"required,description=Path to the file to send"`
		UserID string `json:"user_id,omitempty" jsonschema:"description=WeChat user ID to send to (optional)"`
	}
	return fantasy.NewAgentTool(
		WeChatSendFileToolName,
		"Send a file to a WeChat user via the active WeChat account.",
		func(ctx context.Context, in input, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			sender := m.ActiveSender()
			if sender == nil || !sender.IsLoggedIn() {
				return fantasy.ToolResponse{}, fmt.Errorf("no active WeChat account - please login first")
			}
			_ = in.Path
			return fantasy.ToolResponse{Content: "File send not yet implemented - use send_wechat_image for images or text messages for now."}, nil
		},
	)
}
