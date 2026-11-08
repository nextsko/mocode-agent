// Package ssh hosts the remote-execution tools (ssh_exec, ssh_upload,
// ssh_download, ssh_list_hosts) over the shared sshcommon connection pool.
package ssh

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/core/permission"
	"github.com/nextsko/mocode-agent/internal/core/tools/contract"
	sshcommon "github.com/nextsko/mocode-agent/internal/core/tools/external/common/ssh"
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

// NewSshExecPortTool is the framework-agnostic form of the ssh_exec tool: it
// runs a single command on a remote host.
func NewSshExecPortTool(svc *sshcommon.Service, permissions permission.Service) contract.Tool {
	return contract.NewFunc(
		sshcommon.SshExecToolName,
		toolutil.FirstLineDescription(sshExecDescription),
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"host": map[string]any{
					"type":        "string",
					"description": "SSH host: alias from ~/.ssh/config, 'user@host', or 'user@host:port'",
				},
				"command": map[string]any{
					"type":        "string",
					"description": "The shell command to execute on the remote host",
				},
				"working_dir": map[string]any{
					"type":        "string",
					"description": "Remote working directory (defaults to the remote user's home)",
				},
				"timeout_secs": map[string]any{
					"type":        "integer",
					"description": "Kill the command after this many seconds (default 30)",
				},
				"description": map[string]any{
					"type":        "string",
					"description": "A short, human-readable description of what this command does",
				},
			},
			"required": []string{"host", "command"},
		},
		func(ctx context.Context, _ contract.ToolContext, args json.RawMessage) (contract.ToolResult, error) {
			var params SshExecParams
			if err := json.Unmarshal(args, &params); err != nil {
				return contract.ToolResult{}, fmt.Errorf("invalid parameters: %w", err)
			}
			callID, _ := toolutil.ToolCallIDFromCtx(ctx)
			return runSshExec(ctx, svc, permissions, params, callID)
		},
	)
}

// runSshExec validates the request, asks for permission and executes the
// command over the shared connection pool.
func runSshExec(
	ctx context.Context,
	svc *sshcommon.Service,
	permissions permission.Service,
	params SshExecParams,
	callID string,
) (contract.ToolResult, error) {
	if params.Host == "" {
		return contract.Error("host is required"), nil
	}
	if params.Command == "" {
		return contract.Error("command is required"), nil
	}
	if reason := sshcommon.IsCommandBanned(params.Command); reason != "" {
		return contract.Error("command blocked by policy: " + reason), nil
	}

	sessionID, err := sshcommon.MustSessionID(ctx)
	if err != nil {
		return contract.ToolResult{}, err
	}

	granted, err := sshcommon.RequestPermission(
		ctx, permissions, sessionID,
		sshcommon.SshExecToolName, "execute", params.Host,
		fmt.Sprintf("Run command on %s: %s", params.Host, sshcommon.TruncateForPrompt(params.Command, 80)),
		callID, SshExecPermissionsParams(params),
	)
	if err != nil {
		return contract.ToolResult{}, err
	}
	if !granted {
		return permissionDenied(), nil
	}

	client, spec, err := svc.AcquireClient(ctx, params.Host)
	if err != nil {
		return contract.Error(sshcommon.DescribeFailure("connect", params.Host, err)), nil
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
		return contract.Error(sshcommon.DescribeFailure("exec", params.Host, err)), nil
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
		return contract.WithMetadata(contract.Text(body), meta), nil
	}
	text := body + "\n<exit_code>" + strconv.Itoa(result.ExitCode) + "</exit_code>"
	return contract.WithMetadata(contract.Error(text), meta), nil
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
