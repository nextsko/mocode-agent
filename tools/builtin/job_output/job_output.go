package job_output

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/package-register/mocode/internal/core/shellruntime/shell"
	"github.com/package-register/mocode/tools"
)

const (
	JobOutputToolName = "job_output"
	bashNoOutput      = "no output"
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

type jobOutputTool struct{}

func New() tools.Tool { return &jobOutputTool{} }

func (t *jobOutputTool) Name() string { return JobOutputToolName }

func (t *jobOutputTool) Description() string { return firstLine(jobOutputDescription) }

func (t *jobOutputTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        JobOutputToolName,
		Description: t.Description(),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"shell_id": map[string]any{
					"type":        "string",
					"description": "The ID of the background shell to retrieve output from",
				},
				"wait": map[string]any{
					"type":        "boolean",
					"description": "If true, block until the background shell completes before returning output",
				},
			},
			"required": []string{"shell_id"},
		},
	}
}

func (t *jobOutputTool) Execute(ctx context.Context, _ tools.ToolContext, args json.RawMessage) (tools.ToolResult, error) {
	var params JobOutputParams
	if err := json.Unmarshal(args, &params); err != nil {
		return tools.ErrorResult(err)
	}
	if params.ShellID == "" {
		return tools.ToolResult{Error: errors.New("missing shell_id")}, nil
	}

	bgManager := shell.GetBackgroundShellManager()
	bgShell, ok := bgManager.Get(params.ShellID)
	if !ok {
		return tools.ToolResult{Error: fmt.Errorf("background shell not found: %s", params.ShellID)}, nil
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
	if output == "" {
		output = bashNoOutput
	}

	summary := fmt.Sprintf("Status: %s (elapsed %.1fs)", jobStatus.State, float64(jobStatus.ElapsedMs)/1000)
	if !done && jobStatus.Interactive {
		summary += " - interactive: use the job_input tool to send stdin"
	}

	return tools.ToolResult{
		Content: fmt.Sprintf("%s\n\n%s", summary, output),
		Metadata: map[string]any{
			"shell_id":          params.ShellID,
			"command":           bgShell.Command,
			"description":       bgShell.Description,
			"done":              done,
			"state":             string(jobStatus.State),
			"exit_code":         jobStatus.ExitCode,
			"elapsed_ms":        jobStatus.ElapsedMs,
			"interactive":       jobStatus.Interactive,
			"working_directory": bgShell.WorkingDir,
		},
	}, nil
}

func firstLine(content []byte) string {
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

func init() { tools.Register(JobOutputToolName, New()) }
