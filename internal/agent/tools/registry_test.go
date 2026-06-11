package tools

import (
	"slices"
	"sort"
	"testing"

	"github.com/package-register/mocode/internal/agent/tools/plugins/gitops"
	"github.com/package-register/mocode/internal/agent/tools/plugins/ssh"
	"github.com/package-register/mocode/internal/knowledge/memory"
	"github.com/stretchr/testify/assert"
)

// knownAllToolNames mirrors config.allToolNames().
// It is kept here as the authoritative source of truth that config consumes.
// Any divergence will be caught by TestAllToolNames_MatchesConfigList.
var knownAllToolNames = []string{
	AgentToolName,
	BashToolName,
	MocodeInfoToolName,
	MocodeLogsToolName,
	JobOutputToolName,
	JobKillToolName,
	DownloadToolName,
	EditToolName,
	MultiEditToolName,
	DiagnosticsToolName,
	ReferencesToolName,
	LSPRestartToolName,
	FetchToolName,
	AgenticFetchToolName,
	GlobToolName,
	GrepToolName,
	LSToolName,
	SourcegraphToolName,
	ThinkToolName,
	TodosToolName,
	ViewToolName,
	WriteToolName,
	ListMCPResourcesToolName,
	ReadMCPResourceToolName,
	CrawlToolName,
	DownloadDocsToolName,
	TransferToolName,
	SessionExportToolName,
	MessageExportToolName,
	SessionSummaryToolName,
	ReadFilesToolName,
	memory.AddToolName,
	memory.UpdateToolName,
	memory.DeleteToolName,
	memory.ClearToolName,
	memory.SearchToolName,
	memory.LoadToolName,
	GiteaIssuesToolName,
	GiteaPullsToolName,
	GiteaNotificationsToolName,
	gitops.PlanCommitsToolName,
	gitops.ExecuteCommitsToolName,
	ssh.SshExecToolName,
	ssh.SshUploadToolName,
	ssh.SshDownloadToolName,
	ssh.SshListHostsToolName,
}

// TestAllToolNames_MatchesConfigList asserts that the registry's standard tool
// names plus the coordinator-owned tool names equal the canonical tool list
// maintained in config.allToolNames().
func TestAllToolNames_MatchesConfigList(t *testing.T) {
	t.Parallel()

	// Registry names (standard tools).
	registryNames := AllToolNames()

	// Add coordinator-owned tools not built by the registry.
	coordNames := coordinatorToolNames()
	for _, d := range coordNames {
		registryNames = append(registryNames, d.Name)
	}

	// Sort both slices for stable comparison.
	sort.Strings(registryNames)
	expected := make([]string, len(knownAllToolNames))
	copy(expected, knownAllToolNames)
	sort.Strings(expected)

	assert.Equal(t, expected, registryNames, "registry tool names must match config.allToolNames()")
}

// TestAllToolDescriptors_NoDuplicates ensures no tool name appears twice.
func TestAllToolDescriptors_NoDuplicates(t *testing.T) {
	t.Parallel()

	seen := make(map[string]struct{})
	for _, d := range AllToolDescriptors() {
		_, dup := seen[d.Name]
		assert.Falsef(t, dup, "duplicate tool name %q in registry", d.Name)
		seen[d.Name] = struct{}{}
	}
}

// TestToolCategory_KnownCategories ensures every descriptor has a non-empty category.
func TestToolCategory_KnownCategories(t *testing.T) {
	t.Parallel()

	for _, d := range AllToolDescriptors() {
		assert.NotEmptyf(t, d.Category, "tool %q has empty category", d.Name)
	}
}

// TestBuiltinTools_ContainCoreSet verifies that builtin tools include the
// essential file and exec tools an agent always needs.
func TestBuiltinTools_ContainCoreSet(t *testing.T) {
	t.Parallel()

	coreBuiltins := []string{
		BashToolName,
		EditToolName,
		ViewToolName,
		WriteToolName,
		LSToolName,
	}

	descs := AllToolDescriptors()
	for _, name := range coreBuiltins {
		idx := slices.IndexFunc(descs, func(d ToolDescriptor) bool {
			return d.Name == name
		})
		assert.GreaterOrEqualf(t, idx, 0, "builtin tool %q missing from registry", name)
		if idx >= 0 {
			assert.Equalf(t, ToolKindBuiltin, descs[idx].Kind, "tool %q should be ToolKindBuiltin", name)
		}
	}
}
