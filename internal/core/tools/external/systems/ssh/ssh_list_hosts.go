package ssh

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/core/tools/contract"
	sshcommon "github.com/nextsko/mocode-agent/internal/core/tools/external/common/ssh"
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

// NewSshListHostsPortTool is the framework-agnostic form of the ssh_list_hosts
// tool: it enumerates the entries in ~/.ssh/config.  It does not require the
// permission service because it only reads local config — no remote
// side-effects.
func NewSshListHostsPortTool(svc *sshcommon.Service) contract.Tool {
	return contract.NewFunc(
		sshcommon.SshListHostsToolName,
		toolutil.FirstLineDescription(sshListHostsDescription),
		map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
		func(_ context.Context, _ contract.ToolContext, _ json.RawMessage) (contract.ToolResult, error) {
			return runSshListHosts(), nil
		},
	)
}

// runSshListHosts renders the host table for the model.
func runSshListHosts() contract.ToolResult {
	path := sshcommon.ConfigPath()
	if path == "" {
		return contract.Error("could not resolve $HOME")
	}
	if !sshcommon.FileExists(path) {
		return contract.Text(fmt.Sprintf("No ssh config found at %s", path))
	}

	data, err := sshcommon.ReadFile(path)
	if err != nil {
		return contract.Error(fmt.Sprintf("read config: %v", err))
	}

	hosts := sshcommon.ParseSshConfig(string(data))
	if len(hosts) == 0 {
		return contract.Text(fmt.Sprintf("No Host entries found in %s", path))
	}

	// Stable ordering for deterministic output.
	sort.Slice(hosts, func(i, j int) bool { return hosts[i].Alias < hosts[j].Alias })

	var b strings.Builder
	fmt.Fprintf(&b, "Found %d host entries in %s:\n\n", len(hosts), path)
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
			fmt.Fprintf(&b, "%d", h.Port)
		}
		b.WriteString(" | ")
		b.WriteString(h.IdentityFile)
		b.WriteString(" | ")
		b.WriteString(h.ProxyJump)
		b.WriteString(" |\n")
	}

	meta := SshListHostsResponseMetadata{ConfigPath: path, Hosts: hosts}
	return contract.WithMetadata(contract.Text(b.String()), meta)
}
