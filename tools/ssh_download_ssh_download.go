package tools

import (
	"context"
	_ "embed"
	"fmt"
	"os"

	"charm.land/fantasy"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/core/permission"
	"github.com/nextsko/mocode-agent/tools/plugins/sshcommon"
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

// NewSshDownloadTool returns a tool that copies remote → local via scp.
func NewSshDownloadTool(svc *sshcommon.Service, permissions permission.Service) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		sshcommon.SshDownloadToolName,
		toolutil.FirstLineDescription(sshDownloadDescription),
		func(ctx context.Context, params SshDownloadParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.Host == "" {
				return fantasy.NewTextErrorResponse("host is required"), nil
			}
			if params.Local == "" || params.Remote == "" {
				return fantasy.NewTextErrorResponse("local and remote are required"), nil
			}
			if !sshcommon.IsPathSafe(params.Remote) {
				return fantasy.NewTextErrorResponse("remote path must not contain '..'"), nil
			}

			if _, err := os.Stat(params.Local); err == nil {
				return fantasy.NewTextErrorResponse(
					fmt.Sprintf("local path already exists: %s (delete it first)", params.Local)), nil
			}

			sessionID, err := sshcommon.MustSessionID(ctx)
			if err != nil {
				return fantasy.ToolResponse{}, err
			}

			granted, err := sshcommon.RequestPermission(
				ctx, permissions, sessionID,
				sshcommon.SshDownloadToolName, "download", params.Host,
				fmt.Sprintf("Download %s:%s → %s", params.Host, params.Remote, params.Local),
				call, SshDownloadPermissionsParams(params),
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

			if err := client.Download(ctx, params.Remote, params.Local); err != nil {
				return fantasy.NewTextErrorResponse(sshcommon.DescribeFailure("download", params.Host, err)), nil
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
			return fantasy.WithResponseMetadata(fantasy.NewTextResponse(body), meta), nil
		},
	)
}
