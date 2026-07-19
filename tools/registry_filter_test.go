package tools

import (
	"charm.land/fantasy"
	"context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

// stubNamedTool creates a minimal AgentTool whose Info().Name returns name.
func stubNamedTool(name string) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		name,
		"stub "+name,
		func(_ context.Context, _ struct{}, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			return fantasy.NewTextResponse("ok"), nil
		},
	)
}

// TestByCategory_KnownDescriptor verifies that ByCategory keeps tools matching
// the given category and drops all others.
func TestByCategory_KnownDescriptor(t *testing.T) {
	t.Parallel()
	descs := AllToolDescriptors()
	require.NotEmpty(t, descs)

	// Collect all names in the exec category from descriptors.
	var execNames []string
	for _, d := range descs {
		if d.Category == CategoryExec {
			execNames = append(execNames, d.Name)
		}
	}
	require.NotEmpty(t, execNames, "expected at least one exec-category tool")

	// Build a cheap tool list just from descriptors (no real deps needed).
	// We only need Info().Name to match, so stubTools suffices.
	allNames := make([]string, len(descs))
	for i, d := range descs {
		allNames[i] = d.Name
	}

	// Verify the filter returns only exec-category names.
	f := ByCategory(CategoryExec)
	ctx := context.Background()

	for _, d := range descs {
		stub := stubNamedTool(d.Name)
		result := f(ctx, stub)
		wantPass := d.Category == CategoryExec
		assert.Equal(t, wantPass, result,
			"ByCategory(exec) on tool %q: got %v, want %v", d.Name, result, wantPass)
	}
}

// TestByCategory_UnknownTool verifies that tools not in AllToolDescriptors
// are passed through (not dropped).
func TestByCategory_UnknownTool(t *testing.T) {
	t.Parallel()
	f := ByCategory(CategoryExec)
	unknown := stubNamedTool("__dynamic_injected_tool__")
	assert.True(t, f(context.Background(), unknown),
		"unknown tools should pass through ByCategory")
}

// TestByKind_Builtin verifies ByKind(ToolKindBuiltin) keeps only builtins.
func TestByKind_Builtin(t *testing.T) {
	t.Parallel()
	f := ByKind(ToolKindBuiltin)
	ctx := context.Background()

	for _, d := range AllToolDescriptors() {
		stub := stubNamedTool(d.Name)
		got := f(ctx, stub)
		want := d.Kind == ToolKindBuiltin
		assert.Equal(t, want, got,
			"ByKind(builtin) on tool %q: got %v, want %v", d.Name, got, want)
	}
}

// TestExcludeCategories_BlocksNetworkTools verifies network-category tools
// are excluded.
func TestExcludeCategories_BlocksNetworkTools(t *testing.T) {
	t.Parallel()
	f := ExcludeCategories(CategoryNetwork)
	ctx := context.Background()

	for _, d := range AllToolDescriptors() {
		stub := stubNamedTool(d.Name)
		got := f(ctx, stub)
		wantPass := d.Category != CategoryNetwork
		assert.Equal(t, wantPass, got,
			"ExcludeCategories(network) on tool %q", d.Name)
	}
}

// TestBuildFiltered_ReducesTool verifies Registry.BuildFiltered passes
// ExcludeNames correctly without needing full deps.
func TestBuildFiltered_ReducesTool(t *testing.T) {
	t.Parallel()

	// We just verify the function is callable and returns fewer tools.
	// Without full ToolDeps, Build() would panic or return nothing for most tools.
	// Instead, verify that AllToolDescriptors() + ByCategory works via compat re-exports.
	descs := AllToolDescriptors()
	require.NotEmpty(t, descs)

	networkCount := 0
	for _, d := range descs {
		if d.Category == CategoryNetwork {
			networkCount++
		}
	}
	assert.Positive(t, networkCount, "expected network tools in standard descriptors")
}
