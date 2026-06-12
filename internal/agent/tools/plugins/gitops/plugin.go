package gitops

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/fantasy"
	"github.com/package-register/mocode/internal/agent/toolutil/shared"
)

// ── Tool names ─────────────────────────────────────────────────────────────────

const (
	PlanCommitsToolName    = "git_plan_commits"
	ExecuteCommitsToolName = "git_execute_commits"
)

// ── Embedded descriptions ──────────────────────────────────────────────────────

//go:embed plan_commits.md
var planDescription []byte

//go:embed execute_commits.md
var executeDescription []byte

// ── Params ─────────────────────────────────────────────────────────────────────

type PlanCommitsParams struct {
	// No parameters needed — auto-scans the working tree.
}

type ExecuteCommitEntry struct {
	Index   int    `json:"index"   description:"Index of the group from plan_commits output"`
	Subject string `json:"subject" description:"Commit message subject in format: emoji type(scope): description"`
	Body    string `json:"body,omitempty" description:"Optional detailed commit body (bullet points)"`
}

type ExecuteCommitsParams struct {
	Commits []ExecuteCommitEntry `json:"commits" description:"Ordered list of commits to execute"`
}

// ── Response metadata ──────────────────────────────────────────────────────────

type PlanResponseMetadata struct {
	Plan *CommitPlan `json:"plan"`
}

type ExecuteResultEntry struct {
	Index   int    `json:"index"`
	Hash    string `json:"hash"`
	Subject string `json:"subject"`
	Files   int    `json:"files"`
}

type ExecuteResponseMetadata struct {
	Results []ExecuteResultEntry `json:"results"`
	Total   int                  `json:"total"`
}

// ── Tool constructors ──────────────────────────────────────────────────────────

// NewPlanCommitsTool scans the working tree and returns a structured commit plan.
func NewPlanCommitsTool(workingDir string) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		PlanCommitsToolName,
		shared.FirstLineDescription(planDescription),
		func(_ context.Context, _ PlanCommitsParams, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if workingDir == "" {
				return fantasy.NewTextErrorResponse("no working directory configured"), nil
			}

			result, err := ScanWorkingTree(workingDir)
			if err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("scan failed: %v", err)), nil
			}

			if len(result.Files) == 0 {
				return fantasy.NewTextResponse("Nothing to commit — working tree is clean."), nil
			}

			plan := GroupFiles(result)
			text := FormatPlan(plan)

			return fantasy.WithResponseMetadata(
				fantasy.NewTextResponse(text),
				PlanResponseMetadata{Plan: plan},
			), nil
		},
	)
}

// NewExecuteCommitsTool executes an ordered list of commits.
func NewExecuteCommitsTool(workingDir string) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		ExecuteCommitsToolName,
		shared.FirstLineDescription(executeDescription),
		func(_ context.Context, params ExecuteCommitsParams, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if workingDir == "" {
				return fantasy.NewTextErrorResponse("no working directory configured"), nil
			}

			if len(params.Commits) == 0 {
				return fantasy.NewTextErrorResponse("no commits provided"), nil
			}

			// Validate all subjects are non-empty
			for _, c := range params.Commits {
				if strings.TrimSpace(c.Subject) == "" {
					return fantasy.NewTextErrorResponse(
						fmt.Sprintf("commit group %d has empty subject", c.Index)), nil
				}
			}

			// Scan to get current file list
			scan, err := ScanWorkingTree(workingDir)
			if err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("scan failed: %v", err)), nil
			}

			plan := GroupFiles(scan)
			results := make([]ExecuteResultEntry, 0, len(params.Commits))

			for _, entry := range params.Commits {
				// Find the matching group
				if entry.Index < 0 || entry.Index >= len(plan.Groups) {
					return fantasy.NewTextErrorResponse(
						fmt.Sprintf("invalid group index %d (plan has %d groups)",
							entry.Index, len(plan.Groups))), nil
				}

				group := plan.Groups[entry.Index]

				// Stage files
				for _, f := range group.Files {
					if _, err := gitRun(workingDir, "add", "--", f); err != nil {
						return fantasy.NewTextErrorResponse(
							fmt.Sprintf("git add failed for %s: %v", f, err)), nil
					}
				}

				// Build commit message
				args := []string{"commit", "-m", entry.Subject}
				if strings.TrimSpace(entry.Body) != "" {
					args = []string{"commit", "-m", entry.Subject, "-m", entry.Body}
				}

				out, err := gitRun(workingDir, args...)
				if err != nil {
					return fantasy.NewTextErrorResponse(
						fmt.Sprintf("git commit failed for group %d: %v\n%s",
							entry.Index, err, string(out))), nil
				}

				// Get the commit hash
				hashOut, _ := gitRun(workingDir, "rev-parse", "--short", "HEAD")
				hash := strings.TrimSpace(string(hashOut))

				results = append(results, ExecuteResultEntry{
					Index:   entry.Index,
					Hash:    hash,
					Subject: entry.Subject,
					Files:   len(group.Files),
				})
			}

			// Final status check
			statusOut, _ := gitRun(workingDir, "status", "--short")
			remaining := strings.TrimSpace(string(statusOut))
			remainingCount := 0
			if remaining != "" {
				remainingCount = len(strings.Split(remaining, "\n"))
			}

			// Build summary text
			var b strings.Builder
			b.WriteString(fmt.Sprintf("✅ %d commits created:\n", len(results)))
			for _, r := range results {
				b.WriteString(fmt.Sprintf("  %s %s (%d files)\n", r.Hash, r.Subject, r.Files))
			}
			if remainingCount > 0 {
				b.WriteString(fmt.Sprintf("\n⚠️ %d file(s) still uncommitted", remainingCount))
			} else {
				b.WriteString("\n🎉 Working tree is clean!")
			}

			meta := ExecuteResponseMetadata{
				Results: results,
				Total:   len(results),
			}
			metaJSON, _ := json.Marshal(meta)

			return fantasy.WithResponseMetadata(
				fantasy.NewTextResponse(b.String()),
				json.RawMessage(metaJSON),
			), nil
		},
	)
}
