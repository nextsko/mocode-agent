package ssh_exec

import (
	"context"
	_ "embed"
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/fantasy"

	"github.com/package-register/mocode/internal/core/agent/tools/plugins/sshcommon"
	"github.com/package-register/mocode/internal/core/agent/toolutil"
	"github.com/package-register/mocode/internal/core/permission"
)

//go:embed ssh_exec.md
var sshExecDescription []byte

// SshExecParams are the inputs the LLM fills in to call the tool.
type SshExecParams struct {
	Host        string `json:"host"          description:"SSH host: alias from ~/.ssh/config, 'user@host', or 'user@host:port'"`
	Command     string `json:"command"       description:"The shell command to execute on the remote host"`
	WorkingDir  string `json:"working_dir,omitempty"  description:"Remote working directory (defaults to the remote user's home)"`
	TimeoutSecs int    `json:"timeout_secs,omitempty" description:"Kill the command after this many seconds (default 30)"`
	Description string `json:"description,omitempty"  description:"A short, human-readable description of what this command does"`
}

// SshExecPermissionsParams is the JSON payload sent to the permission service.
type SshExecPermissionsParams struct {
	Host        string `json:"host"`
	Command     string `json:"command"`
	WorkingDir  string `json:"working_dir,omitempty"`
	TimeoutSecs int    `json:"timeout_secs,omitempty"`
	Description string `json:"description,omitempty"`
}

// SshExecResponseMetadata is the structured part of the tool response.
type SshExecResponseMetadata struct {
	Host       string `json:"host"`
	Command    string `json:"command"`
	ExitCode   int    `json:"exit_code"`
	Stdout     string `json:"stdout,omitempty"`
	Stderr     string `json:"stderr,omitempty"`
	DurationMs int64  `json:"duration_ms"`
	TimedOut   bool   `json:"timed_out,omitempty"`
	WorkingDir string `json:"working_dir,omitempty"`
}

// NewSshExecTool returns a fantasy.AgentTool that runs a single command on a remote host.
func NewSshExecTool(svc *sshcommon.Service, permissions permission.Service) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		sshcommon.SshExecToolName,
		toolutil.FirstLineDescription(sshExecDescription),
		func(ctx context.Context, params SshExecParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.Host == "" {
				return fantasy.NewTextErrorResponse("host is required"), nil
			}
			if params.Command == "" {
				return fantasy.NewTextErrorResponse("command is required"), nil
			}
			if reason := sshcommon.IsCommandBanned(params.Command); reason != "" {
				return fantasy.NewTextErrorResponse(
					"command blocked by policy: " + reason), nil
			}

			sessionID, err := sshcommon.MustSessionID(ctx)
			if err != nil {
				return fantasy.ToolResponse{}, err
			}

			granted, err := sshcommon.RequestPermission(
				ctx, permissions, sessionID,
				sshcommon.SshExecToolName, "execute", params.Host,
				fmt.Sprintf("Run command on %s: %s", params.Host, sshcommon.TruncateForPrompt(params.Command, 80)),
				call, SshExecPermissionsParams(params),
			)
			if err != nil {
				return fantasy.ToolResponse{}, err
			}
			if !granted {
				return toolutil.NewPermissionDeniedResponse(), nil
			}

			client, spec, err := svc.AcquireClient(ctx, params.Host)
			if err != nil {
				return fantasy.NewTextErrorResponse(sshcommon.DescribeFailure("connect", params.Host, err)), nil
			}
			defer svc.Pool().Put(client)

			remoteCmd := params.Command
			if params.WorkingDir != "" {
				remoteCmd = fmt.Sprintf("cd %q && %s", params.WorkingDir, params.Command)
			}
			timeout := time.Duration(params.TimeoutSecs) * time.Second
			if timeout <= 0 {
				timeout = 30 * time.Second
			}

			result, err := client.Exec(ctx, remoteCmd, timeout)
			if err != nil {
				return fantasy.NewTextErrorResponse(sshcommon.DescribeFailure("exec", params.Host, err)), nil
			}

			meta := SshExecResponseMetadata{
				Host:       spec.String(),
				Command:    params.Command,
				ExitCode:   result.ExitCode,
				Stdout:     result.Stdout,
				Stderr:     result.Stderr,
				DurationMs: result.Took.Milliseconds(),
				TimedOut:   result.ExitCode == 124,
				WorkingDir: params.WorkingDir,
			}

			body := formatExecResult(params.Host, result)
			if result.ExitCode == 0 {
				return fantasy.WithResponseMetadata(fantasy.NewTextResponse(body), meta), nil
			}
			text := body + "\n<exit_code>" + strconv.Itoa(result.ExitCode) + "</exit_code>"
			return fantasy.WithResponseMetadata(
				fantasy.NewTextErrorResponse(text), meta), nil
		},
	)
}

func formatExecResult(host string, r *sshcommon.ExecResult) string {
	var b strings.Builder
	b.WriteString("<ssh host=\"")
	b.WriteString(host)
	b.WriteString("\" exit_code=\"")
	b.WriteString(strconv.Itoa(r.ExitCode))
	b.WriteString("\" duration_ms=\"")
	b.WriteString(strconv.FormatInt(r.Took.Milliseconds(), 10))
	b.WriteString("\">\n")
	if r.Stdout != "" {
		b.WriteString("<stdout>\n")
		b.WriteString(sshcommon.TruncateForDisplay(r.Stdout))
		b.WriteString("\n</stdout>\n")
	}
	if r.Stderr != "" {
		b.WriteString("<stderr>\n")
		b.WriteString(sshcommon.TruncateForDisplay(r.Stderr))
		b.WriteString("\n</stderr>\n")
	}
	if r.ExitCode == 124 {
		b.WriteString("\n<timed_out>true</timed_out>\n")
	}
	b.WriteString("</ssh>")
	return b.String()
}
