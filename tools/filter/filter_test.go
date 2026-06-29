package filter_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/package-register/mocode/tools"
	"github.com/package-register/mocode/tools/filter"
)

// ─── stubs ────────────────────────────────────────────────────────────────────
//
// All stubs are unexported. They exist solely to exercise the framework-agnostic
// filter contract defined in tools/contracts.go; they MUST NOT import
// charm.land/fantasy or anything under internal/core/agent.
//
// stubTool satisfies both tools.Tool and tools.Capable.
// nonCapableTool satisfies only tools.Tool — explicitly NOT tools.Capable.
// stubToolContext is a minimal tools.ToolContext for tests that need to
// pass a context to Apply or observe that a FilterFunc received it.

// stubTool implements tools.Tool + tools.Capable.
type stubTool struct {
	name string
	caps []tools.Capability
}

func (s *stubTool) Name() string        { return s.name }
func (s *stubTool) Description() string { return s.name }
func (s *stubTool) Schema() tools.Schema {
	return tools.Schema{Name: s.name}
}
func (s *stubTool) Capabilities() []tools.Capability { return s.caps }
func (s *stubTool) Execute(_ context.Context, _ tools.ToolContext, _ json.RawMessage) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}

// nonCapableTool implements tools.Tool only — no Capabilities() method,
// so it does not satisfy tools.Capable.
type nonCapableTool struct{ name string }

func (s *nonCapableTool) Name() string        { return s.name }
func (s *nonCapableTool) Description() string { return s.name }
func (s *nonCapableTool) Schema() tools.Schema {
	return tools.Schema{Name: s.name}
}

func (s *nonCapableTool) Execute(_ context.Context, _ tools.ToolContext, _ json.RawMessage) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}

// stubToolContext is the request-scoped context used to verify context
// propagation through Apply. PermissionChecker / MCPHandles / ToolCallbacks
// return nil because no filter under test reads them; this keeps the stub
// honest — only the surface the filter actually depends on is meaningful.
type stubToolContext struct {
	sessID  string
	workDir string
}

func (c *stubToolContext) SessionID() string  { return c.sessID }
func (c *stubToolContext) WorkingDir() string { return c.workDir }
func (c *stubToolContext) Permissions() tools.PermissionChecker {
	return nil
}
func (c *stubToolContext) MCP() tools.MCPHandles          { return nil }
func (c *stubToolContext) Callbacks() tools.ToolCallbacks { return nil }

// ─── helpers ──────────────────────────────────────────────────────────────────

func toolNames(ts []tools.Tool) []string {
	names := make([]string, len(ts))
	for i, tool := range ts {
		names[i] = tool.Name()
	}
	return names
}

// ─── Apply: ordering & predicate basics ───────────────────────────────────────

// TestApply_EmptyFns verifies that Apply with no FilterFuncs preserves the
// input tool list (every tool passes an empty predicate set).
func TestApply_EmptyFns(t *testing.T) {
	t.Parallel()
	all := []tools.Tool{
		&stubTool{name: "a"},
		&stubTool{name: "b"},
		&stubTool{name: "c"},
	}
	tctx := &stubToolContext{sessID: "s1"}
	got := filter.Apply(tctx, all)
	require.Equal(t, []string{"a", "b", "c"}, toolNames(got))
}

// TestApply_KeepAll verifies that an always-true FilterFunc retains every
// input tool in original order.
func TestApply_KeepAll(t *testing.T) {
	t.Parallel()
	all := []tools.Tool{
		&stubTool{name: "a"},
		&stubTool{name: "b"},
	}
	tctx := &stubToolContext{}
	keep := filter.FilterFunc(func(_ tools.ToolContext, _ tools.Tool) bool { return true })
	got := filter.Apply(tctx, all, keep)
	require.Equal(t, []string{"a", "b"}, toolNames(got))
}

// TestApply_DropAll verifies that an always-false FilterFunc drops every
// input tool, returning an empty (non-nil) slice.
func TestApply_DropAll(t *testing.T) {
	t.Parallel()
	all := []tools.Tool{
		&stubTool{name: "a"},
		&stubTool{name: "b"},
	}
	tctx := &stubToolContext{}
	drop := filter.FilterFunc(func(_ tools.ToolContext, _ tools.Tool) bool { return false })
	got := filter.Apply(tctx, all, drop)
	require.Empty(t, got)
}

// TestApply_PreservesOrder verifies that surviving tools keep their
// original input order even when the filter would otherwise select them
// out of order.
func TestApply_PreservesOrder(t *testing.T) {
	t.Parallel()
	all := []tools.Tool{
		&stubTool{name: "c"},
		&stubTool{name: "a"},
		&stubTool{name: "b"},
		&stubTool{name: "d"},
	}
	tctx := &stubToolContext{}
	got := filter.Apply(tctx, all, filter.IncludeNames("b", "a"))
	require.Equal(t, []string{"a", "b"}, toolNames(got))
}

// TestApply_MultipleFns verifies AND semantics across multiple FilterFuncs:
// a tool is kept only if every filter returns true for it.
func TestApply_MultipleFns(t *testing.T) {
	t.Parallel()
	all := []tools.Tool{
		&stubTool{name: "bash"},
		&stubTool{name: "view"},
		&stubTool{name: "fetch"},
	}
	tctx := &stubToolContext{}
	got := filter.Apply(
		tctx, all,
		filter.IncludeNames("bash", "view"),
		filter.ExcludeNames("view"),
	)
	require.Equal(t, []string{"bash"}, toolNames(got))
}

// ─── Chain ────────────────────────────────────────────────────────────────────

// TestChain verifies that Chain(f1, f2) composed into one FilterFunc is
// equivalent to passing both as variadic arguments to Apply.
func TestChain(t *testing.T) {
	t.Parallel()
	all := []tools.Tool{
		&stubTool{name: "bash"},
		&stubTool{name: "view"},
		&stubTool{name: "fetch"},
	}
	tctx := &stubToolContext{}
	chained := filter.Chain(
		filter.IncludeNames("bash", "view"),
		filter.ExcludeNames("view"),
	)
	got := filter.Apply(tctx, all, chained)
	require.Equal(t, []string{"bash"}, toolNames(got))
}

// ─── IncludeNames / ExcludeNames ──────────────────────────────────────────────

// TestIncludeNames verifies that IncludeNames keeps only tools whose
// Name() appears in the listed set and drops the rest.
func TestIncludeNames(t *testing.T) {
	t.Parallel()
	all := []tools.Tool{
		&stubTool{name: "bash"},
		&stubTool{name: "view"},
		&stubTool{name: "fetch"},
	}
	tctx := &stubToolContext{}
	got := filter.Apply(tctx, all, filter.IncludeNames("bash", "view"))
	require.Equal(t, []string{"bash", "view"}, toolNames(got))
}

// TestIncludeNames_Empty verifies that an empty list of names keeps
// nothing: nothing can match.
func TestIncludeNames_Empty(t *testing.T) {
	t.Parallel()
	all := []tools.Tool{
		&stubTool{name: "bash"},
		&stubTool{name: "view"},
	}
	tctx := &stubToolContext{}
	got := filter.Apply(tctx, all, filter.IncludeNames())
	require.Empty(t, got)
}

// TestExcludeNames verifies that ExcludeNames drops the listed tools and
// keeps the rest in their original order.
func TestExcludeNames(t *testing.T) {
	t.Parallel()
	all := []tools.Tool{
		&stubTool{name: "bash"},
		&stubTool{name: "view"},
		&stubTool{name: "fetch"},
	}
	tctx := &stubToolContext{}
	got := filter.Apply(tctx, all, filter.ExcludeNames("fetch"))
	require.Equal(t, []string{"bash", "view"}, toolNames(got))
}

// ─── IncludeCapabilities ──────────────────────────────────────────────────────

// TestIncludeCapabilities_ToolWithCap verifies that a tool implementing
// tools.Capable and advertising one of the requested capabilities is kept.
func TestIncludeCapabilities_ToolWithCap(t *testing.T) {
	t.Parallel()
	all := []tools.Tool{
		&stubTool{name: "fs-read", caps: []tools.Capability{"fs.read"}},
		&stubTool{name: "net-fetch", caps: []tools.Capability{"net.fetch"}},
	}
	tctx := &stubToolContext{}
	got := filter.Apply(tctx, all, filter.IncludeCapabilities("fs.read"))
	require.Equal(t, []string{"fs-read"}, toolNames(got))
}

// TestIncludeCapabilities_ToolWithoutCap verifies that a tool that does
// NOT implement tools.Capable is dropped: requesting a capability against a
// tool that declares none cannot succeed.
func TestIncludeCapabilities_ToolWithoutCap(t *testing.T) {
	t.Parallel()
	all := []tools.Tool{
		&nonCapableTool{name: "bare"},
		&stubTool{name: "with-cap", caps: []tools.Capability{"fs.read"}},
	}
	tctx := &stubToolContext{}
	got := filter.Apply(tctx, all, filter.IncludeCapabilities("fs.read"))
	require.Equal(t, []string{"with-cap"}, toolNames(got))
}

// TestIncludeCapabilities_MultipleCaps verifies OR semantics across the
// requested capability set: a tool is kept if it advertises ANY one of
// the listed capabilities.
func TestIncludeCapabilities_MultipleCaps(t *testing.T) {
	t.Parallel()
	all := []tools.Tool{
		&stubTool{name: "a", caps: []tools.Capability{"fs.read"}},
		&stubTool{name: "b", caps: []tools.Capability{"net.fetch"}},
		&stubTool{name: "c", caps: []tools.Capability{"fs.write"}},
	}
	tctx := &stubToolContext{}
	got := filter.Apply(tctx, all, filter.IncludeCapabilities("fs.read", "net.fetch"))
	require.Equal(t, []string{"a", "b"}, toolNames(got))
}

// ─── BySource (no-op under empty source meta) ────────────────────────────────

// TestBySource_NoOpWhenSourceEmpty verifies that BySource is currently a
// no-op: per Task 1.4, source meta is empty everywhere until Task 2.1 fills
// it in, so BySource("anything") must let every tool through.
func TestBySource_NoOpWhenSourceEmpty(t *testing.T) {
	t.Parallel()
	all := []tools.Tool{
		&stubTool{name: "a"},
		&stubTool{name: "b"},
		&stubTool{name: "c"},
	}
	tctx := &stubToolContext{}
	got := filter.Apply(tctx, all, filter.BySource("anything"))
	require.Equal(t, []string{"a", "b", "c"}, toolNames(got))
}

// ─── Context propagation ──────────────────────────────────────────────────────

// TestFilterFunc_ReceivesContext verifies that the FilterFunc passed to Apply
// receives the same tools.ToolContext the caller handed in — both by
// observable value and by pointer identity through the interface.
func TestFilterFunc_ReceivesContext(t *testing.T) {
	t.Parallel()
	all := []tools.Tool{
		&stubTool{name: "bash"},
		&stubTool{name: "fetch"},
	}
	tctxIn := &stubToolContext{sessID: "session-1", workDir: "/tmp"}

	var observed tools.ToolContext
	fn := filter.FilterFunc(func(tctx tools.ToolContext, _ tools.Tool) bool {
		observed = tctx
		return true
	})
	got := filter.Apply(tctxIn, all, fn)

	// Pointer identity: the interface wraps the very object we passed in.
	observedConcrete, ok := observed.(*stubToolContext)
	require.True(t, ok, "observed context should be *stubToolContext")
	require.Same(t, tctxIn, observedConcrete)

	// Value echo: the context's identifying fields survive the trip.
	require.Equal(t, "session-1", observedConcrete.SessionID())
	require.Equal(t, "/tmp", observedConcrete.WorkingDir())

	// And Apply still returned every tool because our filter is always true.
	require.Equal(t, []string{"bash", "fetch"}, toolNames(got))
}
