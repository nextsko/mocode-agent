package shell

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/core/shellruntime/shell"
	"github.com/nextsko/mocode-agent/internal/core/tools/contract"
)

const (
	JobOutputToolName = "job_output"
)

//go:embed job_output.md
var jobOutputDescription []byte

const (
	// jobOutputMaxWait bounds the blocking wait so a never-finishing job
	// (server, prompt) can no longer hang the whole agent turn.
	jobOutputMaxWait = 2 * time.Minute
	// jobOutputTailBytes is how much of each stream a poll returns. Chatty
	// jobs used to ship their entire log history into context on every call.
	jobOutputTailBytes = 16 * 1024
	// jobOutputPollInterval is the wait-loop tick that re-checks status so a
	// detected prompt can return early instead of sleeping the full budget.
	jobOutputPollInterval = 100 * time.Millisecond
)

type JobOutputParams struct {
	ShellID string `json:"shell_id" description:"The ID of the background shell to retrieve output from"`
	Wait    bool   `json:"wait" description:"If true, block until the shell completes, the wait timeout hits, or the job looks like it is waiting for interactive input (then returns immediately with the prompt visible)"`
	// WaitTimeoutSec bounds how long Wait=true blocks (default 60, max 120).
	// A job that runs longer returns with its latest tail output and state.
	WaitTimeoutSec int `json:"wait_timeout_sec,omitempty" description:"Max seconds to block when wait=true (default 60, capped at 120); returns early on completion or detected interactive prompt"`
}

type JobOutputResponseMetadata struct {
	ShellID          string `json:"shell_id"`
	Command          string `json:"command"`
	Description      string `json:"description"`
	Done             bool   `json:"done"`
	State            string `json:"state"`
	ExitCode         int    `json:"exit_code,omitempty"`
	ElapsedMs        int64  `json:"elapsed_ms"`
	IdleMs           int64  `json:"idle_ms,omitempty"`
	LikelyPrompting  bool   `json:"likely_prompting,omitempty"`
	Interactive      bool   `json:"interactive,omitempty"`
	WorkingDirectory string `json:"working_directory"`
}

// NewJobOutputPortTool is the framework-agnostic form of the job_output tool.
// It operates on the process-wide background job manager, exactly like the
// legacy constructor did, so job lifecycle semantics are unchanged.
func NewJobOutputPortTool() contract.Tool {
	return contract.NewFunc(
		JobOutputToolName,
		toolutil.FirstLineDescription(jobOutputDescription),
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"shell_id": map[string]any{
					"type":        "string",
					"description": "The ID of the background shell to retrieve output from",
				},
				"wait": map[string]any{
					"type":        "boolean",
					"description": "If true, block until the shell completes, the wait timeout hits, or the job looks like it is waiting for interactive input (then returns immediately with the prompt visible)",
				},
				"wait_timeout_sec": map[string]any{
					"type":        "integer",
					"description": "Max seconds to block when wait=true (default 60, capped at 120); returns early on completion or detected interactive prompt",
				},
			},
			"required": []string{"shell_id", "wait"},
		},
		func(ctx context.Context, _ contract.ToolContext, args json.RawMessage) (contract.ToolResult, error) {
			var params JobOutputParams
			if len(args) > 0 {
				if err := json.Unmarshal(args, &params); err != nil {
					return contract.ToolResult{}, fmt.Errorf("invalid parameters: %w", err)
				}
			}
			return runJobOutput(ctx, params)
		},
	)
}

// runJobOutput polls one background shell and renders its latest tail output.
func runJobOutput(ctx context.Context, params JobOutputParams) (contract.ToolResult, error) {
	if params.ShellID == "" {
		return contract.Error("missing shell_id"), nil
	}

	bgManager := shell.GetBackgroundShellManager()
	bgShell, ok := bgManager.Get(params.ShellID)
	if !ok || !bgShell.BelongsTo(toolutil.GetSessionFromContext(ctx)) {
		return contract.Error(fmt.Sprintf("background shell not found: %s", params.ShellID)), nil
	}

	if params.Wait {
		timeout := time.Duration(params.WaitTimeoutSec) * time.Second
		if timeout <= 0 {
			timeout = 60 * time.Second
		}
		if timeout > jobOutputMaxWait {
			timeout = jobOutputMaxWait
		}
		// Bounded wait: return on completion, ctx cancellation, budget
		// expiry — or EARLY when the job goes quiet and its output
		// looks like an interactive prompt, which used to hang the
		// turn forever with nobody able to tell why.
		deadline := time.After(timeout)
		ticker := time.NewTicker(jobOutputPollInterval)
		defer ticker.Stop()
	waitLoop:
		for {
			status := bgShell.Status()
			if status.Done || status.LikelyPrompting {
				break waitLoop
			}
			select {
			case <-ticker.C:
				continue
			case <-deadline:
				break waitLoop
			case <-ctx.Done():
				break waitLoop
			}
		}
	}

	stdout, stderr, truncated, done, err := bgShell.GetTailOutput(jobOutputTailBytes, jobOutputTailBytes)
	jobStatus := bgShell.Status()

	var outputParts []string
	if truncated {
		outputParts = append(outputParts, fmt.Sprintf("[showing last %d bytes per stream; %d bytes total]", jobOutputTailBytes, jobStatus.StdoutBytes+jobStatus.StderrBytes))
	}
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
		IdleMs:           jobStatus.IdleMs,
		LikelyPrompting:  jobStatus.LikelyPrompting,
		Interactive:      jobStatus.Interactive,
		WorkingDirectory: bgShell.WorkingDir,
	}

	if output == "" {
		output = BashNoOutput
	}

	// Lead with the structured state so the model can observe the job
	// at a glance: what it is doing, how long, and — the key signal —
	// whether it looks blocked on user input.
	summary := fmt.Sprintf("Status: %s (elapsed %.1fs, idle %.1fs)", jobStatus.State, float64(jobStatus.ElapsedMs)/1000, float64(jobStatus.IdleMs)/1000)
	switch {
	case jobStatus.LikelyPrompting:
		summary += " - LIKELY WAITING FOR INPUT: the job went quiet and its last output looks like a prompt. Read the output above and answer it with the job_input tool (or job_kill to abort)."
	case !done && (jobStatus.TTY || jobStatus.Interactive):
		summary += " - accepts stdin via the job_input tool"
	}
	result := fmt.Sprintf("%s\n\n%s", summary, output)
	return contract.WithMetadata(contract.Text(result), metadata), nil
}
