package job_kill

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/package-register/mocode/internal/core/shellruntime/shell"
	"github.com/package-register/mocode/tools"
)

const JobKillToolName = "job_kill"

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

type jobKillTool struct{}

func New() tools.Tool { return &jobKillTool{} }

func (t *jobKillTool) Name() string { return JobKillToolName }

func (t *jobKillTool) Description() string { return string(jobKillDescription) }

func (t *jobKillTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        JobKillToolName,
		Description: t.Description(),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"shell_id": map[string]any{
					"type":        "string",
					"description": "The ID of the background shell to terminate",
				},
			},
			"required": []string{"shell_id"},
		},
	}
}

func (t *jobKillTool) Execute(_ context.Context, _ tools.ToolContext, args json.RawMessage) (tools.ToolResult, error) {
	var params JobKillParams
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

	metadata := map[string]any{
		"shell_id":    params.ShellID,
		"command":     bgShell.Command,
		"description": bgShell.Description,
	}
	if err := bgManager.Kill(params.ShellID); err != nil {
		return tools.ErrorResultWithMetadata(err, metadata)
	}

	return tools.ToolResult{
		Content:  fmt.Sprintf("Background shell %s terminated successfully", params.ShellID),
		Metadata: metadata,
	}, nil
}

func init() { tools.Register(JobKillToolName, New()) }
