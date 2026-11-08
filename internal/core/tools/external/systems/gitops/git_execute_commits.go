package gitops

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/core/tools/contract"
	gitopscommon "github.com/nextsko/mocode-agent/internal/core/tools/external/common/gitops"
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

// NewExecuteCommitsPortTool is the framework-agnostic form of
// git_execute_commits: it executes an ordered list of commits.
func NewExecuteCommitsPortTool(workingDir string) contract.Tool {
	return contract.NewFunc(
		ExecuteCommitsToolName,
		toolutil.FirstLineDescription(executeDescription),
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"commits": map[string]any{
					"type":        "array",
					"description": "Ordered list of commits to execute",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"index": map[string]any{
								"type":        "integer",
								"description": "Index of the group from plan_commits output",
							},
							"subject": map[string]any{
								"type":        "string",
								"description": "Commit message subject in format: emoji type(scope): description",
							},
							"body": map[string]any{
								"type":        "string",
								"description": "Optional detailed commit body (bullet points)",
							},
						},
						"required": []string{"index", "subject"},
					},
				},
			},
			"required": []string{"commits"},
		},
		func(ctx context.Context, _ contract.ToolContext, args json.RawMessage) (contract.ToolResult, error) {
			var params ExecuteCommitsParams
			if err := json.Unmarshal(args, &params); err != nil {
				return contract.ToolResult{}, fmt.Errorf("invalid parameters: %w", err)
			}
			return runExecuteCommits(ctx, workingDir, params), nil
		},
	)
}

// runExecuteCommits applies each planned commit group in order and reports what
// happened.
func runExecuteCommits(ctx context.Context, workingDir string, params ExecuteCommitsParams) contract.ToolResult {
	if workingDir == "" {
		return contract.Error("no working directory configured")
	}

	if len(params.Commits) == 0 {
		return contract.Error("no commits provided")
	}

	for _, c := range params.Commits {
		if strings.TrimSpace(c.Subject) == "" {
			return contract.Error(
				fmt.Sprintf("commit group %d has empty subject", c.Index))
		}
	}

	scan, err := gitopscommon.ScanWorkingTree(ctx, workingDir)
	if err != nil {
		return contract.Error(fmt.Sprintf("scan failed: %v", err))
	}

	plan := gitopscommon.GroupFiles(scan)
	results := make([]ExecuteResultEntry, 0, len(params.Commits))

	for _, entry := range params.Commits {
		if entry.Index < 0 || entry.Index >= len(plan.Groups) {
			return contract.Error(
				fmt.Sprintf("invalid group index %d (plan has %d groups)",
					entry.Index, len(plan.Groups)))
		}

		group := plan.Groups[entry.Index]

		for _, f := range group.Files {
			if _, err := gitopscommon.GitRun(ctx, workingDir, "add", "--", f); err != nil {
				return contract.Error(
					fmt.Sprintf("git add failed for %s: %v", f, err))
			}
		}

		args := []string{"commit", "-m", entry.Subject}
		if strings.TrimSpace(entry.Body) != "" {
			args = []string{"commit", "-m", entry.Subject, "-m", entry.Body}
		}

		out, err := gitopscommon.GitRun(ctx, workingDir, args...)
		if err != nil {
			return contract.Error(
				fmt.Sprintf("git commit failed for group %d: %v\n%s",
					entry.Index, err, string(out)))
		}

		hashOut, _ := gitopscommon.GitRun(ctx, workingDir, "rev-parse", "--short", "HEAD")
		hash := strings.TrimSpace(string(hashOut))

		results = append(results, ExecuteResultEntry{
			Index:   entry.Index,
			Hash:    hash,
			Subject: entry.Subject,
			Files:   len(group.Files),
		})
	}

	statusOut, _ := gitopscommon.GitRun(ctx, workingDir, "status", "--short")
	remaining := strings.TrimSpace(string(statusOut))
	remainingCount := 0
	if remaining != "" {
		remainingCount = len(strings.Split(remaining, "\n"))
	}

	var b strings.Builder
	fmt.Fprintf(&b, "✅ %d commits created:\n", len(results))
	for _, r := range results {
		fmt.Fprintf(&b, "  %s %s (%d files)\n", r.Hash, r.Subject, r.Files)
	}
	if remainingCount > 0 {
		fmt.Fprintf(&b, "\n⚠️ %d file(s) still uncommitted", remainingCount)
	} else {
		b.WriteString("\n🎉 Working tree is clean!")
	}

	meta := ExecuteResponseMetadata{
		Results: results,
		Total:   len(results),
	}
	return contract.WithMetadata(contract.Text(b.String()), meta)
}
