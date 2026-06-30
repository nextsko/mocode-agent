package job_input

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

const JobInputToolName = "job_input"

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

type jobInputTool struct{}

func New() tools.Tool { return &jobInputTool{} }

func (t *jobInputTool) Name() string { return JobInputToolName }

func (t *jobInputTool) Description() string { return firstLine(jobInputDescription) }

func (t *jobInputTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        JobInputToolName,
		Description: t.Description(),
		Parameters: map[string]any{
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
	}
}

func (t *jobInputTool) Execute(_ context.Context, _ tools.ToolContext, args json.RawMessage) (tools.ToolResult, error) {
	var params JobInputParams
	if err := json.Unmarshal(args, &params); err != nil {
		return tools.ErrorResult(err)
	}
	if params.ShellID == "" {
		return tools.ToolResult{Error: errors.New("missing shell_id")}, nil
	}
	if params.Input == "" && !params.PressEnter {
		return tools.ToolResult{Error: errors.New("missing input")}, nil
	}

	bgManager := shell.GetBackgroundShellManager()
	bgShell, ok := bgManager.Get(params.ShellID)
	if !ok {
		return tools.ToolResult{Error: fmt.Errorf("background shell not found: %s", params.ShellID)}, nil
	}

	status := bgShell.Status()
	if status.Done {
		return tools.ToolResult{Error: fmt.Errorf("background shell %s is %s and no longer accepts input", params.ShellID, status.State)}, nil
	}

	payload := []byte(params.Input)
	if params.PressEnter {
		payload = append(payload, '\n')
	}
	n, err := bgShell.WriteInput(payload)
	if err != nil {
		return tools.ErrorResult(err)
	}

	return tools.ToolResult{
		Content: fmt.Sprintf("Wrote %d byte(s) to background shell %s. Use job_output to read the response.", n, params.ShellID),
		Metadata: map[string]any{
			"shell_id": params.ShellID,
			"state":    string(status.State),
			"written":  n,
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

func init() { tools.Register(JobInputToolName, New()) }
