package ssh

import (
	"context"
	_ "embed"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"charm.land/fantasy"
	"github.com/package-register/mocode/internal/agent/tools/internal/shared"
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
type HostInfo struct {
	Alias        string `json:"alias"`
	HostName     string `json:"hostname"`
	User         string `json:"user,omitempty"`
	Port         int    `json:"port,omitempty"`
	IdentityFile string `json:"identity_file,omitempty"`
	ProxyJump    string `json:"proxy_jump,omitempty"`
}

// NewSshListHostsTool returns a tool that enumerates the entries in
// ~/.ssh/config.  It does not require the permission service because
// it only reads local config — no remote side-effects.
func NewSshListHostsTool(svc *Service) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		SshListHostsToolName,
		shared.FirstLineDescription(sshListHostsDescription),
		func(_ context.Context, _ SshListHostsParams, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			path := ConfigPath()
			if path == "" {
				return fantasy.NewTextErrorResponse("could not resolve $HOME"), nil
			}
			if !fileExists(path) {
				return fantasy.NewTextResponse(
					fmt.Sprintf("No ssh config found at %s", path)), nil
			}

			data, err := readFile(path)
			if err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("read config: %v", err)), nil
			}

			hosts := parseSshConfig(string(data))
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

// parseSshConfig performs a minimal ssh_config(5) parser.  It is good
// enough for the well-known subset of directives (Host, HostName, User,
// Port, IdentityFile, ProxyJump) and intentionally rejects nothing —
// unrecognised lines are ignored.
func parseSshConfig(content string) []HostInfo {
	var hosts []HostInfo
	var current *HostInfo

	flush := func() {
		if current != nil && current.Alias != "" && !isWildcard(current.Alias) {
			hosts = append(hosts, *current)
		}
		current = nil
	}

	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		key := fields[0]
		val := strings.TrimSpace(strings.TrimPrefix(line, key))

		switch key {
		case "Host":
			flush()
			// First token of Host line is the alias; later tokens are
			// alternate patterns which we ignore for simplicity.
			current = &HostInfo{Alias: fields[1]}
		case "HostName":
			if current != nil {
				current.HostName = val
			}
		case "User":
			if current != nil {
				current.User = val
			}
		case "Port":
			if current != nil {
				if p, err := strconv.Atoi(strings.TrimSpace(val)); err == nil {
					current.Port = p
				}
			}
		case "IdentityFile":
			if current != nil {
				current.IdentityFile = val
			}
		case "ProxyJump":
			if current != nil {
				current.ProxyJump = val
			}
		}
	}
	flush()
	return hosts
}

func isWildcard(s string) bool {
	return strings.ContainsAny(s, "*?")
}
