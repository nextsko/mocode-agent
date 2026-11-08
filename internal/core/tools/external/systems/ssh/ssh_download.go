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

//go:embed ssh_download.md
var sshDownloadDescription []byte

// SshDownloadParams is what the LLM sends.
type SshDownloadParams struct {
	Host   string `json:"host"   description:"SSH host: alias from ~/.ssh/config, 'user@host', or 'user@host:port'"`
	Remote string `json:"remote" description:"Absolute path on the remote host"`
	Local  string `json:"local"  description:"Local path to write the file to"`
}

// SshDownloadPermissionsParams is the permission payload.
type SshDownloadPermissionsParams struct {
	Host   string `json:"host"`
	Remote string `json:"remote"`
	Local  string `json:"local"`
}

// SshDownloadResponseMetadata is the structured response.
type SshDownloadResponseMetadata struct {
	Host      string `json:"host"`
	Remote    string `json:"remote"`
	Local     string `json:"local"`
	BytesRead int64  `json:"bytes_read"`
}

// NewSshDownloadPortTool is the framework-agnostic form of the ssh_download
// tool: it copies a remote file to the local filesystem.
func NewSshDownloadPortTool(svc *sshcommon.Service, permissions permission.Service) contract.Tool {
	return contract.NewFunc(
		sshcommon.SshDownloadToolName,
		toolutil.FirstLineDescription(sshDownloadDescription),
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"host": map[string]any{
					"type":        "string",
					"description": "SSH host: alias from ~/.ssh/config, 'user@host', or 'user@host:port'",
				},
				"remote": map[string]any{
					"type":        "string",
					"description": "Absolute path on the remote host",
				},
				"local": map[string]any{
					"type":        "string",
					"description": "Local path to write the file to",
				},
			},
			"required": []string{"host", "remote", "local"},
		},
		func(ctx context.Context, _ contract.ToolContext, args json.RawMessage) (contract.ToolResult, error) {
			var params SshDownloadParams
			if err := json.Unmarshal(args, &params); err != nil {
				return contract.ToolResult{}, fmt.Errorf("invalid parameters: %w", err)
			}
			callID, _ := toolutil.ToolCallIDFromCtx(ctx)
			return runSshDownload(ctx, svc, permissions, params, callID)
		},
	)
}

// runSshDownload validates the destination, asks for permission and downloads
// the remote file.
func runSshDownload(
	ctx context.Context,
	svc *sshcommon.Service,
	permissions permission.Service,
	params SshDownloadParams,
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

	if _, err := os.Stat(params.Local); err == nil {
		return contract.Error(
			fmt.Sprintf("local path already exists: %s (delete it first)", params.Local)), nil
	}

	sessionID, err := sshcommon.MustSessionID(ctx)
	if err != nil {
		return contract.ToolResult{}, err
	}

	granted, err := sshcommon.RequestPermission(
		ctx, permissions, sessionID,
		sshcommon.SshDownloadToolName, "download", params.Host,
		fmt.Sprintf("Download %s:%s → %s", params.Host, params.Remote, params.Local),
		callID, SshDownloadPermissionsParams(params),
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

	if err := client.Download(ctx, params.Remote, params.Local); err != nil {
		return contract.Error(sshcommon.DescribeFailure("download", params.Host, err)), nil
	}

	st, _ := os.Stat(params.Local)
	var bytes int64
	if st != nil {
		bytes = st.Size()
	}
	meta := SshDownloadResponseMetadata{
		Host:      spec.String(),
		Remote:    params.Remote,
		Local:     params.Local,
		BytesRead: bytes,
	}
	body := fmt.Sprintf("✅ Downloaded %d bytes from %s:%s to %s",
		bytes, params.Host, params.Remote, params.Local)
	return contract.WithMetadata(contract.Text(body), meta), nil
}
