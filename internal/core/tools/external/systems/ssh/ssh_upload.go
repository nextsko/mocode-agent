package ssh

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/core/permission"
	"github.com/nextsko/mocode-agent/internal/core/tools/contract"
	sshcommon "github.com/nextsko/mocode-agent/internal/core/tools/external/common/ssh"
)

//go:embed ssh_upload.md
var sshUploadDescription []byte

// SshUploadParams is what the LLM sends.
type SshUploadParams struct {
	Host   string `json:"host"   description:"SSH host: alias from ~/.ssh/config, 'user@host', or 'user@host:port'"`
	Local  string `json:"local"  description:"Path to the local file to upload"`
	Remote string `json:"remote" description:"Absolute path on the remote host"`
	Mode   string `json:"mode,omitempty"  description:"Remote file mode in octal (e.g. '0644').  Empty preserves the local mode."`
}

// SshUploadPermissionsParams is the permission payload.
type SshUploadPermissionsParams struct {
	Host   string `json:"host"`
	Local  string `json:"local"`
	Remote string `json:"remote"`
	Mode   string `json:"mode,omitempty"`
}

// SshUploadResponseMetadata is the structured response.
type SshUploadResponseMetadata struct {
	Host      string `json:"host"`
	Local     string `json:"local"`
	Remote    string `json:"remote"`
	BytesSent int    `json:"bytes_sent"`
	Mode      string `json:"mode"`
}

// NewSshUploadPortTool is the framework-agnostic form of the ssh_upload tool:
// it copies a local file to the remote host.
func NewSshUploadPortTool(svc *sshcommon.Service, permissions permission.Service) contract.Tool {
	return contract.NewFunc(
		sshcommon.SshUploadToolName,
		toolutil.FirstLineDescription(sshUploadDescription),
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"host": map[string]any{
					"type":        "string",
					"description": "SSH host: alias from ~/.ssh/config, 'user@host', or 'user@host:port'",
				},
				"local": map[string]any{
					"type":        "string",
					"description": "Path to the local file to upload",
				},
				"remote": map[string]any{
					"type":        "string",
					"description": "Absolute path on the remote host",
				},
				"mode": map[string]any{
					"type":        "string",
					"description": "Remote file mode in octal (e.g. '0644').  Empty preserves the local mode.",
				},
			},
			"required": []string{"host", "local", "remote"},
		},
		func(ctx context.Context, _ contract.ToolContext, args json.RawMessage) (contract.ToolResult, error) {
			var params SshUploadParams
			if err := json.Unmarshal(args, &params); err != nil {
				return contract.ToolResult{}, fmt.Errorf("invalid parameters: %w", err)
			}
			callID, _ := toolutil.ToolCallIDFromCtx(ctx)
			return runSshUpload(ctx, svc, permissions, params, callID)
		},
	)
}

// runSshUpload validates the local file, asks for permission and uploads it.
func runSshUpload(
	ctx context.Context,
	svc *sshcommon.Service,
	permissions permission.Service,
	params SshUploadParams,
	callID string,
) (contract.ToolResult, error) {
	if params.Host == "" {
		return contract.Error("host is required"), nil
	}
	if params.Local == "" || params.Remote == "" {
		return contract.Error("local and remote are required"), nil
	}
	if !sshcommon.IsPathSafe(params.Remote) {
		return contract.Error("remote path must not contain '..'"), nil
	}

	st, err := os.Stat(params.Local)
	if err != nil {
		return contract.Error(fmt.Sprintf("local file: %v", err)), nil
	}
	if st.IsDir() {
		return contract.Error("local path is a directory, not a file"), nil
	}

	sessionID, err := sshcommon.MustSessionID(ctx)
	if err != nil {
		return contract.ToolResult{}, err
	}

	granted, err := sshcommon.RequestPermission(
		ctx, permissions, sessionID,
		sshcommon.SshUploadToolName, "upload", params.Host,
		fmt.Sprintf("Upload %s → %s:%s", params.Local, params.Host, params.Remote),
		callID, SshUploadPermissionsParams(params),
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

	mode, err := parseMode(params.Mode)
	if err != nil {
		return contract.Error(err.Error()), nil
	}

	if err := client.Upload(ctx, params.Local, params.Remote, mode); err != nil {
		return contract.Error(sshcommon.DescribeFailure("upload", params.Host, err)), nil
	}

	meta := SshUploadResponseMetadata{
		Host:      spec.String(),
		Local:     params.Local,
		Remote:    params.Remote,
		BytesSent: int(st.Size()),
		Mode:      params.Mode,
	}
	body := fmt.Sprintf("✅ Uploaded %d bytes from %s to %s:%s",
		st.Size(), params.Local, params.Host, params.Remote)
	return contract.WithMetadata(contract.Text(body), meta), nil
}

func parseMode(s string) (os.FileMode, error) {
	if s == "" {
		return 0, nil
	}
	var m uint32
	for _, c := range s {
		if c < '0' || c > '7' {
			return 0, fmt.Errorf("mode must be octal (got %q)", s)
		}
		m = m*8 + uint32(c-'0')
	}
	return os.FileMode(m), nil
}
