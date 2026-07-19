package tools

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"github.com/package-register/mocode/internal/core/agent/toolutil"

	"charm.land/fantasy"

	"github.com/package-register/mocode/internal/core/shellruntime/shell"
)

const (
	JobOutputToolName = "job_output"
)

//go:embed job_output.md
var jobOutputDescription []byte

type JobOutputParams struct {
	ShellID string `json:"shell_id" description:"The ID of the background shell to retrieve output from"`
	Wait    bool   `json:"wait" description:"If true, block until the background shell completes before returning output"`
}

type JobOutputResponseMetadata struct {
	ShellID          string `json:"shell_id"`
	Command          string `json:"command"`
	Description      string `json:"description"`
	Done             bool   `json:"done"`
	State            string `json:"state"`
	ExitCode         int    `json:"exit_code,omitempty"`
	ElapsedMs        int64  `json:"elapsed_ms"`
	Interactive      bool   `json:"interactive,omitempty"`
	WorkingDirectory string `json:"working_directory"`
}

func NewJobOutputTool() fantasy.AgentTool {
	return fantasy.NewAgentTool(
		JobOutputToolName,
		toolutil.FirstLineDescription(jobOutputDescription),
		func(ctx context.Context, params JobOutputParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.ShellID == "" {
				return fantasy.NewTextErrorResponse("missing shell_id"), nil
			}

			bgManager := shell.GetBackgroundShellManager()
			bgShell, ok := bgManager.Get(params.ShellID)
			if !ok {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("background shell not found: %s", params.ShellID)), nil
			}

			if params.Wait {
				bgShell.WaitContext(ctx)
			}

			stdout, stderr, done, err := bgShell.GetOutput()
			jobStatus := bgShell.Status()

			var outputParts []string
			if stdout != "" {
				outputParts = append(outputParts, stdout)
			}
			if stderr != "" {
				outputParts = append(outputParts, stderr)
			}

			if done {
				if ec := shell.ExitCode(err); ec != 0 {
					outputParts = append(outputParts, fmt.Sprintf("Exit code %d", ec))
				}
			}

			output := strings.Join(outputParts, "\n")

			metadata := JobOutputResponseMetadata{
				ShellID:          params.ShellID,
				Command:          bgShell.Command,
				Description:      bgShell.Description,
				Done:             done,
				State:            string(jobStatus.State),
				ExitCode:         jobStatus.ExitCode,
				ElapsedMs:        jobStatus.ElapsedMs,
				Interactive:      jobStatus.Interactive,
				WorkingDirectory: bgShell.WorkingDir,
			}

			if output == "" {
				output = BashNoOutput
			}

			// Lead with the structured state so the model can observe the job at
			// a glance; hint at the interaction path when the job is still live.
			summary := fmt.Sprintf("Status: %s (elapsed %.1fs)", jobStatus.State, float64(jobStatus.ElapsedMs)/1000)
			if !done && jobStatus.Interactive {
				summary += " - interactive: use the job_input tool to send stdin"
			}
			result := fmt.Sprintf("%s\n\n%s", summary, output)
			return fantasy.WithResponseMetadata(fantasy.NewTextResponse(result), metadata), nil
		})
}
