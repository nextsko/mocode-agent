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

// PullsToolName is the registered name of the gitea_pulls tool.
const PullsToolName = "gitea_pulls"

//go:embed gitea_pulls.md
var pullsDescription []byte

// PullsParams holds input parameters for the gitea_pulls tool.
type PullsParams struct {
	Repo  string `json:"repo,omitempty"  description:"Repository in owner/name format. Defaults to the repo in $PWD."`
	State string `json:"state,omitempty" description:"Filter by state: open, closed, or all. Defaults to open."`
	Limit int    `json:"limit,omitempty" description:"Maximum number of pull requests to return. Defaults to 20."`
}

// NewPullsPortTool is the framework-agnostic form of the gitea_pulls tool: it
// lists Gitea pull requests via the tea CLI.
func NewPullsPortTool() contract.Tool {
	return contract.NewFunc(
		PullsToolName,
		toolutil.FirstLineDescription(pullsDescription),
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"repo": map[string]any{
					"type":        "string",
					"description": "Repository in owner/name format. Defaults to the repo in $PWD.",
				},
				"state": map[string]any{
					"type":        "string",
					"description": "Filter by state: open, closed, or all. Defaults to open.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum number of pull requests to return. Defaults to 20.",
				},
			},
		},
		func(ctx context.Context, _ contract.ToolContext, args json.RawMessage) (contract.ToolResult, error) {
			var params PullsParams
			if err := json.Unmarshal(args, &params); err != nil {
				return contract.ToolResult{}, fmt.Errorf("invalid parameters: %w", err)
			}
			return runPulls(ctx, params), nil
		},
	)
}

// runPulls lists pull requests through the tea CLI and returns its JSON output.
func runPulls(ctx context.Context, params PullsParams) contract.ToolResult {
	if giteacommon.GetTea() == "" {
		return contract.Error(giteacommon.ErrTeaNotFound)
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
			return contract.Text("No pull requests found.")
		}
		return contract.Error(fmt.Sprintf("tea pulls failed: %v", err))
	}

	text := string(out)
	if text == "" || text == "null\n" || text == "[]" || text == "[]\n" {
		return contract.Text("No pull requests found.")
	}
	return contract.Text(text)
}
