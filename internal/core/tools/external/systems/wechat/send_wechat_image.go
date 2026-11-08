package wechat

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/nextsko/mocode-agent/internal/core/tools/contract"
	"github.com/nextsko/mocode-agent/internal/domain/messenger"
)

// WeChatSendImageToolName is the tool name for sending images to WeChat.
const WeChatSendImageToolName = "send_wechat_image"

// SendWeChatImageParams holds the inputs for send_wechat_image.
type SendWeChatImageParams struct {
	Path   string `json:"path" description:"Path to the image file to send"`
	UserID string `json:"user_id,omitempty" description:"WeChat user ID to send to (optional; uses last active user if empty)"`
}

// NewWeChatSendImagePortTool is the framework-agnostic form of the
// send_wechat_image tool.  The messenger port resolves the active account; pass
// messenger.NoopMessenger{} when no integration is wired in.
func NewWeChatSendImagePortTool(m messenger.Messenger) contract.Tool {
	return contract.NewFunc(
		WeChatSendImageToolName,
		"Send an image file to a WeChat user via the active WeChat account.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Path to the image file to send",
				},
				"user_id": map[string]any{
					"type":        "string",
					"description": "WeChat user ID to send to (optional; uses last active user if empty)",
				},
			},
			"required": []string{"path"},
		},
		func(ctx context.Context, _ contract.ToolContext, args json.RawMessage) (contract.ToolResult, error) {
			var params SendWeChatImageParams
			if err := json.Unmarshal(args, &params); err != nil {
				return contract.ToolResult{}, fmt.Errorf("invalid parameters: %w", err)
			}
			return runSendWeChatImage(ctx, m, params)
		},
	)
}

// runSendWeChatImage reads the image and hands it to the active sender.
func runSendWeChatImage(
	ctx context.Context,
	m messenger.Messenger,
	params SendWeChatImageParams,
) (contract.ToolResult, error) {
	sender := m.ActiveSender()
	if sender == nil || !sender.IsLoggedIn() {
		return contract.ToolResult{}, fmt.Errorf("no active WeChat account - please login first")
	}
	if _, err := os.Stat(params.Path); err != nil {
		return contract.ToolResult{}, fmt.Errorf("image file not found: %s", params.Path)
	}
	data, err := os.ReadFile(params.Path)
	if err != nil {
		return contract.ToolResult{}, fmt.Errorf("read image file: %w", err)
	}
	if err := sender.SendImage(ctx, params.UserID, data); err != nil {
		return contract.ToolResult{}, fmt.Errorf("send wechat image: %w", err)
	}
	return contract.Text(fmt.Sprintf("Image sent to WeChat: %s", params.Path)), nil
}
