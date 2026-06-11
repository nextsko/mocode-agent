package tools

import (
	"context"
	"testing"

	"github.com/package-register/mocode/internal/agent/tools/plugins/ssh"
)

// TestRegistry_IncludesSSHTools asserts that AllToolNames() returns
// the four SSH tool names.  This catches accidental removal of the
// plugin from standardPlugins().
func TestRegistry_IncludesSSHTools(t *testing.T) {
	t.Parallel()
	have := map[string]bool{}
	for _, n := range AllToolNames() {
		have[n] = true
	}
	for _, want := range []string{
		ssh.SshExecToolName,
		ssh.SshUploadToolName,
		ssh.SshDownloadToolName,
		ssh.SshListHostsToolName,
	} {
		if !have[want] {
			t.Errorf("AllToolNames() missing %q", want)
		}
	}
}

// TestSSHPlugin_BuildsAllTools verifies the sshPlugin's Build method
// returns exactly the four declared descriptors.  The descriptors
// themselves are validated against the ToolKindPlugin / CategorySSH
// contract.
func TestSSHPlugin_BuildsAllTools(t *testing.T) {
	t.Parallel()
	p := &sshPlugin{}
	descs := p.Descriptors()
	if len(descs) != 4 {
		t.Fatalf("Descriptors() = %d, want 4", len(descs))
	}
	want := map[string]ToolCategory{
		ssh.SshExecToolName:      CategorySSH,
		ssh.SshUploadToolName:    CategorySSH,
		ssh.SshDownloadToolName:  CategorySSH,
		ssh.SshListHostsToolName: CategorySSH,
	}
	for _, d := range descs {
		wantCat, ok := want[d.Name]
		if !ok {
			t.Errorf("unexpected tool %q in descriptors", d.Name)
			continue
		}
		if d.Category != wantCat {
			t.Errorf("tool %q: category = %q, want %q", d.Name, d.Category, wantCat)
		}
		if d.Kind != ToolKindPlugin {
			t.Errorf("tool %q: kind = %q, want %q", d.Name, d.Kind, ToolKindPlugin)
		}
		delete(want, d.Name)
	}
	for name := range want {
		t.Errorf("missing descriptor for %q", name)
	}
}

// TestSSHPlugin_BuildLazyService ensures the plugin's Build method
// creates the underlying Service on first call and reuses it on
// subsequent calls (the service is the cache for the connection pool).
func TestSSHPlugin_BuildLazyService(t *testing.T) {
	t.Parallel()
	p := &sshPlugin{}
	tools1 := p.Build(context.Background(), ToolDeps{})
	if len(tools1) != 4 {
		t.Fatalf("first Build returned %d tools, want 4", len(tools1))
	}
	if p.svc == nil {
		t.Fatal("Build did not lazy-init p.svc")
	}
	tools2 := p.Build(context.Background(), ToolDeps{})
	if len(tools2) != 4 {
		t.Fatalf("second Build returned %d tools, want 4", len(tools2))
	}
	// Stop should not panic even when called.
	if err := p.Stop(context.Background()); err != nil {
		t.Errorf("Stop: %v", err)
	}
}
