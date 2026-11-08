// Package gitops hosts the structured git commit tools: plan_commits
// classifies working-tree changes and execute_commits applies them in order.
package gitops

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/core/tools/contract"
	gitopscommon "github.com/nextsko/mocode-agent/internal/core/tools/external/common/gitops"
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

// NewPlanCommitsPortTool is the framework-agnostic form of git_plan_commits: it
// scans the working tree and returns a structured commit plan.
func NewPlanCommitsPortTool(workingDir string) contract.Tool {
	return contract.NewFunc(
		PlanCommitsToolName,
		toolutil.FirstLineDescription(planDescription),
		map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
		func(ctx context.Context, _ contract.ToolContext, _ json.RawMessage) (contract.ToolResult, error) {
			return runPlanCommits(ctx, workingDir), nil
		},
	)
}

// runPlanCommits scans the working tree and renders the commit plan.
func runPlanCommits(ctx context.Context, workingDir string) contract.ToolResult {
	if workingDir == "" {
		return contract.Error("no working directory configured")
	}

	result, err := gitopscommon.ScanWorkingTree(ctx, workingDir)
	if err != nil {
		return contract.Error(fmt.Sprintf("scan failed: %v", err))
	}

	if len(result.Files) == 0 {
		return contract.Text("Nothing to commit — working tree is clean.")
	}

	plan := gitopscommon.GroupFiles(result)
	text := gitopscommon.FormatPlan(plan)

	return contract.WithMetadata(contract.Text(text), PlanResponseMetadata{Plan: plan})
}
