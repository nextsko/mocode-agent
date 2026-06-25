package job_input

import (
	"context"
	_ "embed"
	"fmt"

	"charm.land/fantasy"

	"github.com/package-register/mocode/internal/core/agent/toolutil"
	"github.com/package-register/mocode/internal/core/shellruntime/shell"
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

func NewJobInputTool() fantasy.AgentTool {
	return fantasy.NewAgentTool(
		JobInputToolName,
		toolutil.FirstLineDescription(jobInputDescription),
		func(ctx context.Context, params JobInputParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.ShellID == "" {
				return fantasy.NewTextErrorResponse("missing shell_id"), nil
			}
			if params.Input == "" && !params.PressEnter {
				return fantasy.NewTextErrorResponse("missing input"), nil
			}

			bgManager := shell.GetBackgroundShellManager()
			bgShell, ok := bgManager.Get(params.ShellID)
			if !ok {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("background shell not found: %s", params.ShellID)), nil
			}

			status := bgShell.Status()
			if status.Done {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("background shell %s is %s and no longer accepts input", params.ShellID, status.State)), nil
			}

			payload := []byte(params.Input)
			if params.PressEnter {
				payload = append(payload, '\n')
			}

			n, err := bgShell.WriteInput(payload)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}

			metadata := JobInputResponseMetadata{
				ShellID: params.ShellID,
				State:   string(status.State),
				Written: n,
			}
			result := fmt.Sprintf("Wrote %d byte(s) to background shell %s. Use job_output to read the response.", n, params.ShellID)
			return fantasy.WithResponseMetadata(fantasy.NewTextResponse(result), metadata), nil
		})
}
