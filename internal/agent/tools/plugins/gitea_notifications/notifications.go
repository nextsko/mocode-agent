package gitea_notifications

import (
	"context"
	_ "embed"
	"fmt"
	"os/exec"

	"charm.land/fantasy"
	"github.com/package-register/mocode/internal/agent/toolutil"
	"github.com/package-register/mocode/internal/agent/tools/plugins/giteacommon"
)

// NotificationsToolName is the registered name of the gitea_notifications tool.
const NotificationsToolName = "gitea_notifications"

//go:embed notifications.md
var notificationsDescription []byte

// NotificationsParams holds input parameters for the gitea_notifications tool.
type NotificationsParams struct {
	Mine   bool   `json:"mine,omitempty"   description:"If true, show notifications across all your repositories instead of only the current one."`
	Types  string `json:"types,omitempty"  description:"Comma-separated subject types to filter by: issue, pull, repository, commit."`
	States string `json:"states,omitempty" description:"Comma-separated states to filter by: pinned, unread, read. Defaults to unread,pinned."`
	Limit  int    `json:"limit,omitempty"  description:"Maximum number of notifications to return. Defaults to 20."`
}

// NewNotificationsTool creates an AgentTool that shows Gitea notifications via tea CLI.
func NewNotificationsTool() fantasy.AgentTool {
	return fantasy.NewAgentTool(
		NotificationsToolName,
		toolutil.FirstLineDescription(notificationsDescription),
		func(ctx context.Context, params NotificationsParams, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if giteacommon.GetTea() == "" {
				return fantasy.NewTextErrorResponse(giteacommon.ErrTeaNotFound), nil
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
					return fantasy.NewTextResponse("No notifications found."), nil
				}
				return fantasy.NewTextErrorResponse(fmt.Sprintf("tea notifications failed: %v", err)), nil
			}

			text := string(out)
			if text == "" || text == "null\n" || text == "[]" || text == "[]\n" {
				return fantasy.NewTextResponse("No notifications found."), nil
			}
			return fantasy.NewTextResponse(text), nil
		},
	)
}
