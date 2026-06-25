package ssh_upload

import (
	"context"
	_ "embed"
	"fmt"
	"os"

	"charm.land/fantasy"

	"github.com/package-register/mocode/internal/core/agent/tools/plugins/sshcommon"
	"github.com/package-register/mocode/internal/core/agent/toolutil"
	"github.com/package-register/mocode/internal/core/permission"
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

// NewSshUploadTool returns a tool that copies local → remote via scp.
func NewSshUploadTool(svc *sshcommon.Service, permissions permission.Service) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		sshcommon.SshUploadToolName,
		toolutil.FirstLineDescription(sshUploadDescription),
		func(ctx context.Context, params SshUploadParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.Host == "" {
				return fantasy.NewTextErrorResponse("host is required"), nil
			}
			if params.Local == "" || params.Remote == "" {
				return fantasy.NewTextErrorResponse("local and remote are required"), nil
			}
			if !sshcommon.IsPathSafe(params.Remote) {
				return fantasy.NewTextErrorResponse("remote path must not contain '..'"), nil
			}

			st, err := os.Stat(params.Local)
			if err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("local file: %v", err)), nil
			}
			if st.IsDir() {
				return fantasy.NewTextErrorResponse("local path is a directory, not a file"), nil
			}

			sessionID, err := sshcommon.MustSessionID(ctx)
			if err != nil {
				return fantasy.ToolResponse{}, err
			}

			granted, err := sshcommon.RequestPermission(
				ctx, permissions, sessionID,
				sshcommon.SshUploadToolName, "upload", params.Host,
				fmt.Sprintf("Upload %s → %s:%s", params.Local, params.Host, params.Remote),
				call, SshUploadPermissionsParams(params),
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

			mode, err := parseMode(params.Mode)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}

			if err := client.Upload(ctx, params.Local, params.Remote, mode); err != nil {
				return fantasy.NewTextErrorResponse(sshcommon.DescribeFailure("upload", params.Host, err)), nil
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
			return fantasy.WithResponseMetadata(fantasy.NewTextResponse(body), meta), nil
		},
	)
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
