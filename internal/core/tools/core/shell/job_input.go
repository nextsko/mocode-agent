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
	JobInputToolName = "job_input"
)

//go:embed job_input.md
var jobInputDescription []byte

type JobInputParams struct {
	ShellID    string `json:"shell_id" description:"The ID of the background shell to send input to"`
	Input      string `json:"input" description:"The text to write to the process stdin"`
	PressEnter bool   `json:"press_enter,omitempty" description:"Append a trailing newline so line-oriented prompts receive the input"`
}

type JobInputResponseMetadata struct {
	ShellID string `json:"shell_id"`
	State   string `json:"state"`
	Written int    `json:"written"`
}

// NewJobInputPortTool is the framework-agnostic form of the job_input tool. It
// operates on the process-wide background job manager, exactly like the legacy
// constructor did, so job lifecycle semantics are unchanged.
func NewJobInputPortTool() contract.Tool {
	return contract.NewFunc(
		JobInputToolName,
		toolutil.FirstLineDescription(jobInputDescription),
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"shell_id": map[string]any{
					"type":        "string",
					"description": "The ID of the background shell to send input to",
				},
				"input": map[string]any{
					"type":        "string",
					"description": "The text to write to the process stdin",
				},
				"press_enter": map[string]any{
					"type":        "boolean",
					"description": "Append a trailing newline so line-oriented prompts receive the input",
				},
			},
			"required": []string{"shell_id", "input"},
		},
		func(ctx context.Context, _ contract.ToolContext, args json.RawMessage) (contract.ToolResult, error) {
			var params JobInputParams
			if len(args) > 0 {
				if err := json.Unmarshal(args, &params); err != nil {
					return contract.ToolResult{}, fmt.Errorf("invalid parameters: %w", err)
				}
			}
			return runJobInput(ctx, params)
		},
	)
}

// runJobInput writes bytes to a live background shell's stdin.
func runJobInput(ctx context.Context, params JobInputParams) (contract.ToolResult, error) {
	if params.ShellID == "" {
		return contract.Error("missing shell_id"), nil
	}
	if params.Input == "" && !params.PressEnter {
		return contract.Error("missing input"), nil
	}

	bgManager := shell.GetBackgroundShellManager()
	bgShell, ok := bgManager.Get(params.ShellID)
	if !ok || !bgShell.BelongsTo(toolutil.GetSessionFromContext(ctx)) {
		return contract.Error(fmt.Sprintf("background shell not found: %s", params.ShellID)), nil
	}

	status := bgShell.Status()
	if status.Done {
		return contract.Error(fmt.Sprintf("background shell %s is %s and no longer accepts input", params.ShellID, status.State)), nil
	}

	payload := []byte(params.Input)
	if params.PressEnter {
		payload = append(payload, '\n')
	}

	n, err := bgShell.WriteInput(payload)
	if err != nil {
		return contract.Error(err.Error()), nil
	}

	metadata := JobInputResponseMetadata{
		ShellID: params.ShellID,
		State:   string(status.State),
		Written: n,
	}
	result := fmt.Sprintf("Wrote %d byte(s) to background shell %s. Use job_output to read the response.", n, params.ShellID)
	return contract.WithMetadata(contract.Text(result), metadata), nil
}
