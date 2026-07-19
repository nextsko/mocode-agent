package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/fantasy"

	"github.com/package-register/mocode/internal/core/agent/toolutil"
	"github.com/package-register/mocode/tools/plugins/gitopscommon"
)

// ExecuteCommitsToolName is the registered name of the git_execute_commits tool.
const ExecuteCommitsToolName = "git_execute_commits"

//go:embed execute_commits.md
var executeDescription []byte

// ExecuteCommitEntry is one commit to execute from a plan.
type ExecuteCommitEntry struct {
	Index   int    `json:"index"   description:"Index of the group from plan_commits output"`
	Subject string `json:"subject" description:"Commit message subject in format: emoji type(scope): description"`
	Body    string `json:"body,omitempty" description:"Optional detailed commit body (bullet points)"`
}

// ExecuteCommitsParams holds input for git_execute_commits.
type ExecuteCommitsParams struct {
	Commits []ExecuteCommitEntry `json:"commits" description:"Ordered list of commits to execute"`
}

// ExecuteResultEntry summarizes one executed commit.
type ExecuteResultEntry struct {
	Index   int    `json:"index"`
	Hash    string `json:"hash"`
	Subject string `json:"subject"`
	Files   int    `json:"files"`
}

// ExecuteResponseMetadata is structured metadata for git_execute_commits.
type ExecuteResponseMetadata struct {
	Results []ExecuteResultEntry `json:"results"`
	Total   int                  `json:"total"`
}

// NewExecuteCommitsTool executes an ordered list of commits.
func NewExecuteCommitsTool(workingDir string) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		ExecuteCommitsToolName,
		toolutil.FirstLineDescription(executeDescription),
		func(_ context.Context, params ExecuteCommitsParams, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if workingDir == "" {
				return fantasy.NewTextErrorResponse("no working directory configured"), nil
			}

			if len(params.Commits) == 0 {
				return fantasy.NewTextErrorResponse("no commits provided"), nil
			}

			for _, c := range params.Commits {
				if strings.TrimSpace(c.Subject) == "" {
					return fantasy.NewTextErrorResponse(
						fmt.Sprintf("commit group %d has empty subject", c.Index)), nil
				}
			}

			scan, err := gitopscommon.ScanWorkingTree(workingDir)
			if err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("scan failed: %v", err)), nil
			}

			plan := gitopscommon.GroupFiles(scan)
			results := make([]ExecuteResultEntry, 0, len(params.Commits))

			for _, entry := range params.Commits {
				if entry.Index < 0 || entry.Index >= len(plan.Groups) {
					return fantasy.NewTextErrorResponse(
						fmt.Sprintf("invalid group index %d (plan has %d groups)",
							entry.Index, len(plan.Groups))), nil
				}

				group := plan.Groups[entry.Index]

				for _, f := range group.Files {
					if _, err := gitopscommon.GitRun(workingDir, "add", "--", f); err != nil {
						return fantasy.NewTextErrorResponse(
							fmt.Sprintf("git add failed for %s: %v", f, err)), nil
					}
				}

				args := []string{"commit", "-m", entry.Subject}
				if strings.TrimSpace(entry.Body) != "" {
					args = []string{"commit", "-m", entry.Subject, "-m", entry.Body}
				}

				out, err := gitopscommon.GitRun(workingDir, args...)
				if err != nil {
					return fantasy.NewTextErrorResponse(
						fmt.Sprintf("git commit failed for group %d: %v\n%s",
							entry.Index, err, string(out))), nil
				}

				hashOut, _ := gitopscommon.GitRun(workingDir, "rev-parse", "--short", "HEAD")
				hash := strings.TrimSpace(string(hashOut))

				results = append(results, ExecuteResultEntry{
					Index:   entry.Index,
					Hash:    hash,
					Subject: entry.Subject,
					Files:   len(group.Files),
				})
			}

			statusOut, _ := gitopscommon.GitRun(workingDir, "status", "--short")
			remaining := strings.TrimSpace(string(statusOut))
			remainingCount := 0
			if remaining != "" {
				remainingCount = len(strings.Split(remaining, "\n"))
			}

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
