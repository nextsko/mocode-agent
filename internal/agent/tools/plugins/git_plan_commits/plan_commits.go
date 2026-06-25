package git_plan_commits

import (
	"context"
	_ "embed"
	"fmt"

	"charm.land/fantasy"
	"github.com/package-register/mocode/internal/agent/toolutil"
	"github.com/package-register/mocode/internal/agent/tools/plugins/gitopscommon"
)

// PlanCommitsToolName is the registered name of the git_plan_commits tool.
const PlanCommitsToolName = "git_plan_commits"

//go:embed plan_commits.md
var planDescription []byte

// PlanCommitsParams holds input for git_plan_commits.
type PlanCommitsParams struct{}

// PlanResponseMetadata is structured metadata for git_plan_commits.
type PlanResponseMetadata struct {
	Plan *gitopscommon.CommitPlan `json:"plan"`
}

// NewPlanCommitsTool scans the working tree and returns a structured commit plan.
func NewPlanCommitsTool(workingDir string) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		PlanCommitsToolName,
		toolutil.FirstLineDescription(planDescription),
		func(_ context.Context, _ PlanCommitsParams, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if workingDir == "" {
				return fantasy.NewTextErrorResponse("no working directory configured"), nil
			}

			result, err := gitopscommon.ScanWorkingTree(workingDir)
			if err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("scan failed: %v", err)), nil
			}

			if len(result.Files) == 0 {
				return fantasy.NewTextResponse("Nothing to commit — working tree is clean."), nil
			}

			plan := gitopscommon.GroupFiles(result)
			text := gitopscommon.FormatPlan(plan)

			return fantasy.WithResponseMetadata(
				fantasy.NewTextResponse(text),
				PlanResponseMetadata{Plan: plan},
			), nil
		},
	)
}
