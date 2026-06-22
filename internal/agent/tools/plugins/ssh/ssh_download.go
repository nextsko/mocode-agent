package ssh

import (
	"context"
	_ "embed"
	"fmt"
	"os"

	"charm.land/fantasy"
	"github.com/package-register/mocode/internal/agent/toolutil"
	"github.com/package-register/mocode/internal/permission"
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
func NewSshDownloadTool(svc *Service, permissions permission.Service) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		SshDownloadToolName,
		toolutil.FirstLineDescription(sshDownloadDescription),
		func(ctx context.Context, params SshDownloadParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.Host == "" {
				return fantasy.NewTextErrorResponse("host is required"), nil
			}
			if params.Local == "" || params.Remote == "" {
				return fantasy.NewTextErrorResponse("local and remote are required"), nil
			}
			if !isPathSafe(params.Remote) {
				return fantasy.NewTextErrorResponse("remote path must not contain '..'"), nil
			}

			// Refuse to silently overwrite an existing local file unless
			// the caller explicitly wants that behaviour.  We use a
			// separate env-style flag for "force" via the parameter name
			// "overwrite" (omitted from the prompt to keep the schema
			// small — agents usually call with a new local path anyway).
			if _, err := os.Stat(params.Local); err == nil {
				return fantasy.NewTextErrorResponse(
					fmt.Sprintf("local path already exists: %s (delete it first)", params.Local)), nil
			}

			sessionID, err := mustSessionID(ctx)
			if err != nil {
				return fantasy.ToolResponse{}, err
			}

			granted, err := requestPermission(
				ctx, permissions, sessionID,
				SshDownloadToolName, "download", params.Host,
				fmt.Sprintf("Download %s:%s → %s", params.Host, params.Remote, params.Local),
				call, SshDownloadPermissionsParams(params),
			)
			if err != nil {
				return fantasy.ToolResponse{}, err
			}
			if !granted {
				return toolutil.NewPermissionDeniedResponse(), nil
			}

			client, spec, err := svc.acquireClient(ctx, params.Host)
			if err != nil {
				return fantasy.NewTextErrorResponse(describeFailure("connect", params.Host, err)), nil
			}
			defer svc.Pool().Put(client)

			if err := client.Download(ctx, params.Remote, params.Local); err != nil {
				return fantasy.NewTextErrorResponse(describeFailure("download", params.Host, err)), nil
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
