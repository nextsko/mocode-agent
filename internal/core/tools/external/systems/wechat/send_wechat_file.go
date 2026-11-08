// Package wechat hosts the WeChat channel tools (send file/image,
// screenshot) over the messenger port; built by the coordinator, not the
// registry.
package wechat

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nextsko/mocode-agent/internal/core/tools/contract"
	"github.com/nextsko/mocode-agent/internal/domain/messenger"
)

// WeChatSendFileToolName is the tool name for sending files to WeChat.
const WeChatSendFileToolName = "send_wechat_file"

// SendWeChatFileParams holds the inputs for send_wechat_file.
type SendWeChatFileParams struct {
	Path   string `json:"path" description:"Path to the file to send"`
	UserID string `json:"user_id,omitempty" description:"WeChat user ID to send to (optional)"`
}

// NewWeChatSendFilePortTool is the framework-agnostic form of the
// send_wechat_file tool.  The messenger port resolves the active account; pass
// messenger.NoopMessenger{} when no integration is wired in.
func NewWeChatSendFilePortTool(m messenger.Messenger) contract.Tool {
	return contract.NewFunc(
		WeChatSendFileToolName,
		"Send a file to a WeChat user via the active WeChat account.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Path to the file to send",
				},
				"user_id": map[string]any{
					"type":        "string",
					"description": "WeChat user ID to send to (optional)",
				},
			},
			"required": []string{"path"},
		},
		func(ctx context.Context, _ contract.ToolContext, args json.RawMessage) (contract.ToolResult, error) {
			var params SendWeChatFileParams
			if err := json.Unmarshal(args, &params); err != nil {
				return contract.ToolResult{}, fmt.Errorf("invalid parameters: %w", err)
			}
			return runSendWeChatFile(ctx, m, params)
		},
	)
}

// runSendWeChatFile resolves the active account and reports the file-send
// placeholder (file transfer is not implemented yet).
func runSendWeChatFile(
	_ context.Context,
	m messenger.Messenger,
	_ SendWeChatFileParams,
) (contract.ToolResult, error) {
	sender := m.ActiveSender()
	if sender == nil || !sender.IsLoggedIn() {
		return contract.ToolResult{}, fmt.Errorf("no active WeChat account - please login first")
	}
	return contract.Text("File send not yet implemented - use send_wechat_image for images or text messages for now."), nil
}
