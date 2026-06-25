package send_wechat_file

import (
	"context"
	"fmt"

	"charm.land/fantasy"

	wechat "github.com/package-register/mocode/internal/integration/wechat"
)

// WeChatSendFileToolName is the tool name for sending files to WeChat.
const WeChatSendFileToolName = "send_wechat_file"

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
			_ = in.Path
			return fantasy.ToolResponse{Content: "File send not yet implemented — use send_wechat_image for images or text messages for now."}, nil
		},
	)
}
