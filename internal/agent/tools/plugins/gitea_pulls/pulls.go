package gitea_pulls

import (
	"context"
	_ "embed"
	"fmt"
	"os/exec"

	"charm.land/fantasy"
	"github.com/package-register/mocode/internal/agent/toolutil"
	"github.com/package-register/mocode/internal/agent/tools/plugins/giteacommon"
)

// PullsToolName is the registered name of the gitea_pulls tool.
const PullsToolName = "gitea_pulls"

//go:embed pulls.md
var pullsDescription []byte

// PullsParams holds input parameters for the gitea_pulls tool.
type PullsParams struct {
	Repo  string `json:"repo,omitempty"  description:"Repository in owner/name format. Defaults to the repo in $PWD."`
	State string `json:"state,omitempty" description:"Filter by state: open, closed, or all. Defaults to open."`
	Limit int    `json:"limit,omitempty" description:"Maximum number of pull requests to return. Defaults to 20."`
}

// NewPullsTool creates an AgentTool that lists Gitea pull requests via tea CLI.
func NewPullsTool() fantasy.AgentTool {
	return fantasy.NewAgentTool(
		PullsToolName,
		toolutil.FirstLineDescription(pullsDescription),
		func(ctx context.Context, params PullsParams, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if giteacommon.GetTea() == "" {
				return fantasy.NewTextErrorResponse(giteacommon.ErrTeaNotFound), nil
			}

			args := []string{"pulls", "list", "--output", "json"}

			if params.Repo != "" {
				args = append(args, "--repo", params.Repo)
			}
			state := params.State
			if state == "" {
				state = "open"
			}
			args = append(args, "--state", state)

			limit := params.Limit
			if limit <= 0 {
				limit = 20
			}
			args = append(args, "--limit", fmt.Sprintf("%d", limit))

			cmd := giteacommon.TeaCmd(ctx, args...)
			out, err := cmd.Output()
			if err != nil {
				if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
					return fantasy.NewTextResponse("No pull requests found."), nil
				}
				return fantasy.NewTextErrorResponse(fmt.Sprintf("tea pulls failed: %v", err)), nil
			}

			text := string(out)
			if text == "" || text == "null\n" || text == "[]" || text == "[]\n" {
				return fantasy.NewTextResponse("No pull requests found."), nil
			}
			return fantasy.NewTextResponse(text), nil
		},
	)
}
