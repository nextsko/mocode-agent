package tools

import (
	"context"
	_ "embed"
	"fmt"
	"os/exec"

	"charm.land/fantasy"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/tools/plugins/giteacommon"
)

// IssuesToolName is the registered name of the gitea_issues tool.
const IssuesToolName = "gitea_issues"

//go:embed issues.md
var issuesDescription []byte

// IssuesParams holds input parameters for the gitea_issues tool.
type IssuesParams struct {
	Repo    string `json:"repo,omitempty"    description:"Repository in owner/name format. Defaults to the repo in $PWD."`
	State   string `json:"state,omitempty"   description:"Filter by state: open, closed, or all. Defaults to open."`
	Keyword string `json:"keyword,omitempty" description:"Search keyword to filter issues by title or body."`
	Labels  string `json:"labels,omitempty"  description:"Comma-separated list of labels to filter by."`
	Limit   int    `json:"limit,omitempty"   description:"Maximum number of issues to return. Defaults to 20."`
}

// NewIssuesTool creates an AgentTool that lists Gitea issues via tea CLI.
func NewIssuesTool() fantasy.AgentTool {
	return fantasy.NewAgentTool(
		IssuesToolName,
		toolutil.FirstLineDescription(issuesDescription),
		func(ctx context.Context, params IssuesParams, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if giteacommon.GetTea() == "" {
				return fantasy.NewTextErrorResponse(giteacommon.ErrTeaNotFound), nil
			}

			args := []string{"issues", "list", "--output", "json"}

			if params.Repo != "" {
				args = append(args, "--repo", params.Repo)
			}
			state := params.State
			if state == "" {
				state = "open"
			}
			args = append(args, "--state", state)

			if params.Keyword != "" {
				args = append(args, "--keyword", params.Keyword)
			}
			if params.Labels != "" {
				args = append(args, "--labels", params.Labels)
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
					return fantasy.NewTextResponse("No issues found."), nil
				}
				return fantasy.NewTextErrorResponse(fmt.Sprintf("tea issues failed: %v", err)), nil
			}

			text := string(out)
			if text == "" || text == "null\n" || text == "[]" || text == "[]\n" {
				return fantasy.NewTextResponse("No issues found."), nil
			}
			return fantasy.NewTextResponse(text), nil
		},
	)
}
