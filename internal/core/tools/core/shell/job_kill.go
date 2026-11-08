package shell

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/core/shellruntime/shell"
	"github.com/nextsko/mocode-agent/internal/core/tools/contract"
)

const (
	JobKillToolName = "job_kill"
)

//go:embed job_kill.md
var jobKillDescription []byte

type JobKillParams struct {
	ShellID string `json:"shell_id" description:"The ID of the background shell to terminate"`
}

type JobKillResponseMetadata struct {
	ShellID     string `json:"shell_id"`
	Command     string `json:"command"`
	Description string `json:"description"`
}

// NewJobKillPortTool is the framework-agnostic form of the job_kill tool. It
// operates on the process-wide background job manager, exactly like the legacy
// constructor did, so job lifecycle semantics are unchanged.
func NewJobKillPortTool() contract.Tool {
	return contract.NewFunc(
		JobKillToolName,
		string(jobKillDescription),
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"shell_id": map[string]any{
					"type":        "string",
					"description": "The ID of the background shell to terminate",
				},
			},
			"required": []string{"shell_id"},
		},
		func(ctx context.Context, _ contract.ToolContext, args json.RawMessage) (contract.ToolResult, error) {
			var params JobKillParams
			if len(args) > 0 {
				if err := json.Unmarshal(args, &params); err != nil {
					return contract.ToolResult{}, fmt.Errorf("invalid parameters: %w", err)
				}
			}
			return runJobKill(ctx, params)
		},
	)
}

// runJobKill terminates one background shell owned by the current session.
func runJobKill(ctx context.Context, params JobKillParams) (contract.ToolResult, error) {
	if params.ShellID == "" {
		return contract.Error("missing shell_id"), nil
	}

	bgManager := shell.GetBackgroundShellManager()

	bgShell, ok := bgManager.Get(params.ShellID)
	if !ok || !bgShell.BelongsTo(toolutil.GetSessionFromContext(ctx)) {
		return contract.Error(fmt.Sprintf("background shell not found: %s", params.ShellID)), nil
	}

	metadata := JobKillResponseMetadata{
		ShellID:     params.ShellID,
		Command:     bgShell.Command,
		Description: bgShell.Description,
	}

	err := bgManager.Kill(params.ShellID)
	if err != nil {
		return contract.Error(err.Error()), nil
	}

	result := fmt.Sprintf("Background shell %s terminated successfully", params.ShellID)
	return contract.WithMetadata(contract.Text(result), metadata), nil
}
