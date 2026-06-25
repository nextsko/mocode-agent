package ssh_list_hosts

import (
	"context"
	_ "embed"
	"fmt"
	"sort"
	"strings"

	"charm.land/fantasy"

	"github.com/package-register/mocode/internal/core/agent/tools/plugins/sshcommon"
	"github.com/package-register/mocode/internal/core/agent/toolutil"
)

//go:embed ssh_list_hosts.md
var sshListHostsDescription []byte

// SshListHostsParams is what the LLM sends.  No required fields —
// listing is by default a global view.
type SshListHostsParams struct {
	// No parameters: the tool just dumps ~/.ssh/config.  Keeping the
	// struct explicit (rather than empty) so the LLM gets a discoverable
	// tool signature.
}

// SshListHostsResponseMetadata is the structured response.
type SshListHostsResponseMetadata struct {
	ConfigPath string     `json:"config_path"`
	Hosts      []HostInfo `json:"hosts"`
}

// HostInfo mirrors the fields the LLM is likely to need.
type HostInfo = sshcommon.HostInfo

// NewSshListHostsTool returns a tool that enumerates the entries in
// ~/.ssh/config.  It does not require the permission service because
// it only reads local config — no remote side-effects.
func NewSshListHostsTool(svc *sshcommon.Service) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		sshcommon.SshListHostsToolName,
		toolutil.FirstLineDescription(sshListHostsDescription),
		func(_ context.Context, _ SshListHostsParams, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			path := sshcommon.ConfigPath()
			if path == "" {
				return fantasy.NewTextErrorResponse("could not resolve $HOME"), nil
			}
			if !sshcommon.FileExists(path) {
				return fantasy.NewTextResponse(
					fmt.Sprintf("No ssh config found at %s", path)), nil
			}

			data, err := sshcommon.ReadFile(path)
			if err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("read config: %v", err)), nil
			}

			hosts := sshcommon.ParseSshConfig(string(data))
			if len(hosts) == 0 {
				return fantasy.NewTextResponse(
					fmt.Sprintf("No Host entries found in %s", path)), nil
			}

			// Stable ordering for deterministic output.
			sort.Slice(hosts, func(i, j int) bool { return hosts[i].Alias < hosts[j].Alias })

			var b strings.Builder
			b.WriteString(fmt.Sprintf("Found %d host entries in %s:\n\n", len(hosts), path))
			b.WriteString("| Alias | HostName | User | Port | IdentityFile | ProxyJump |\n")
			b.WriteString("|---|---|---|---|---|---|\n")
			for _, h := range hosts {
				b.WriteString("| ")
				b.WriteString(h.Alias)
				b.WriteString(" | ")
				b.WriteString(h.HostName)
				b.WriteString(" | ")
				b.WriteString(h.User)
				b.WriteString(" | ")
				if h.Port != 0 {
					b.WriteString(fmt.Sprintf("%d", h.Port))
				}
				b.WriteString(" | ")
				b.WriteString(h.IdentityFile)
				b.WriteString(" | ")
				b.WriteString(h.ProxyJump)
				b.WriteString(" |\n")
			}

			meta := SshListHostsResponseMetadata{ConfigPath: path, Hosts: hosts}
			return fantasy.WithResponseMetadata(fantasy.NewTextResponse(b.String()), meta), nil
		},
	)
}
