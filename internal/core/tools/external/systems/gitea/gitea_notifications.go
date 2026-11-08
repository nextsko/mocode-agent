package gitea

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os/exec"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/core/tools/contract"
	giteacommon "github.com/nextsko/mocode-agent/internal/core/tools/external/common/gitea"
)

// NotificationsToolName is the registered name of the gitea_notifications tool.
const NotificationsToolName = "gitea_notifications"

//go:embed gitea_notifications.md
var notificationsDescription []byte

// NotificationsParams holds input parameters for the gitea_notifications tool.
type NotificationsParams struct {
	Mine   bool   `json:"mine,omitempty"   description:"If true, show notifications across all your repositories instead of only the current one."`
	Types  string `json:"types,omitempty"  description:"Comma-separated subject types to filter by: issue, pull, repository, commit."`
	States string `json:"states,omitempty" description:"Comma-separated states to filter by: pinned, unread, read. Defaults to unread,pinned."`
	Limit  int    `json:"limit,omitempty"  description:"Maximum number of notifications to return. Defaults to 20."`
}

// NewNotificationsPortTool is the framework-agnostic form of the
// gitea_notifications tool: it shows Gitea notifications via the tea CLI.
func NewNotificationsPortTool() contract.Tool {
	return contract.NewFunc(
		NotificationsToolName,
		toolutil.FirstLineDescription(notificationsDescription),
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"mine": map[string]any{
					"type":        "boolean",
					"description": "If true, show notifications across all your repositories instead of only the current one.",
				},
				"types": map[string]any{
					"type":        "string",
					"description": "Comma-separated subject types to filter by: issue, pull, repository, commit.",
				},
				"states": map[string]any{
					"type":        "string",
					"description": "Comma-separated states to filter by: pinned, unread, read. Defaults to unread,pinned.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum number of notifications to return. Defaults to 20.",
				},
			},
		},
		func(ctx context.Context, _ contract.ToolContext, args json.RawMessage) (contract.ToolResult, error) {
			var params NotificationsParams
			if err := json.Unmarshal(args, &params); err != nil {
				return contract.ToolResult{}, fmt.Errorf("invalid parameters: %w", err)
			}
			return runNotifications(ctx, params), nil
		},
	)
}

// runNotifications lists notifications through the tea CLI and returns its JSON
// output.
func runNotifications(ctx context.Context, params NotificationsParams) contract.ToolResult {
	if giteacommon.GetTea() == "" {
		return contract.Error(giteacommon.ErrTeaNotFound)
	}

	args := []string{"notifications", "ls", "--output", "json"}

	if params.Mine {
		args = append(args, "--mine")
	}
	if params.Types != "" {
		args = append(args, "--types", params.Types)
	}
	if params.States != "" {
		args = append(args, "--states", params.States)
	}
	limit := params.Limit
	if limit <= 0 {
		limit = 20
	}
	args = append(args, "--limit", fmt.Sprintf("%d", limit))

	cmd := giteacommon.TeaCmd(ctx, args...)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return contract.Text("No notifications found.")
		}
		return contract.Error(fmt.Sprintf("tea notifications failed: %v", err))
	}

	text := string(out)
	if text == "" || text == "null\n" || text == "[]" || text == "[]\n" {
		return contract.Text("No notifications found.")
	}
	return contract.Text(text)
}
