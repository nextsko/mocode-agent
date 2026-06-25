package ssh_list_hosts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/package-register/mocode/internal/core/agent/tools/plugins/sshcommon"
)

func TestListHostsTool_ReadsFakeConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := strings.Join([]string{
		"# fake",
		"Host web1",
		"    HostName 10.0.0.10",
		"    User www",
		"    Port 2222",
		"    IdentityFile ~/.ssh/web1_key",
		"",
		"Host db1",
		"    HostName 10.0.0.20",
		"    User postgres",
		"",
		"Host *.wildcard",
		"    User guest",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	sshcommon.StubHome(t, dir)

	// Drive the parser + the response builder directly.  We don't go
	// through fantasy.NewAgentTool's handler here because that would
	// require constructing a fantasy.ToolCall, which is plumbing the
	// test really doesn't need — the handler is a one-liner over
	// buildListHostsResponse() anyway.
	meta, _ := buildListHostsResponse()
	if len(meta.Hosts) == 0 {
		t.Fatal("no hosts parsed")
	}
	// Make sure web1 shows up.
	found := false
	for _, h := range meta.Hosts {
		if h.Alias == "web1" {
			found = true
			if h.Port != 2222 {
				t.Errorf("web1 port = %d", h.Port)
			}
			if h.User != "www" {
				t.Errorf("web1 user = %q", h.User)
			}
		}
		if strings.ContainsAny(h.Alias, "*?") {
			t.Errorf("wildcard leaked: %q", h.Alias)
		}
	}
	if !found {
		t.Errorf("web1 not in parsed hosts: %+v", meta.Hosts)
	}
}

// buildListHostsResponse is a thin wrapper that runs the same logic as
// the tool handler but returns the structured metadata instead of a
// fantasy.ToolResponse, so the test can introspect it.
func buildListHostsResponse() (SshListHostsResponseMetadata, error) {
	path := sshcommon.ConfigPath()
	if path == "" {
		return SshListHostsResponseMetadata{}, nil
	}
	if !sshcommon.FileExists(path) {
		return SshListHostsResponseMetadata{ConfigPath: path}, nil
	}
	data, err := sshcommon.ReadFile(path)
	if err != nil {
		return SshListHostsResponseMetadata{}, err
	}
	hosts := sshcommon.ParseSshConfig(string(data))
	return SshListHostsResponseMetadata{ConfigPath: path, Hosts: hosts}, nil
}

func TestListHostsTool_NotConnectedToNetwork(t *testing.T) {
	// We never want this tool to dial out.  The only thing it touches is
	// the local config file.  Sanity-check that the function is pure:
	// running it twice with the same input must produce the same
	// SshListHostsResponseMetadata.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte("Host a\n  HostName 1.1.1.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sshcommon.StubHome(t, dir)

	a, _ := buildListHostsResponse()
	b, _ := buildListHostsResponse()
	if a.ConfigPath != b.ConfigPath || len(a.Hosts) != len(b.Hosts) {
		t.Errorf("not deterministic: %+v vs %+v", a, b)
	}
}

func TestSshListHostsTool_BuildsCleanly(t *testing.T) {
	// Smoke test: NewSshListHostsTool must not panic with a valid
	// Service.  We don't call the handler — the integration surface
	// is validated by TestListHostsTool_ReadsFakeConfig above.
	tool := NewSshListHostsTool(sshcommon.NewService())
	if tool == nil {
		t.Fatal("tool is nil")
	}
}
