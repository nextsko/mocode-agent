// Package tools: registry_test.go — unit tests for the Registry and the
// package-level forwarders. These tests are the "seam" coverage for PR1 of
// the agent-architecture-refactor change: every future tool will register
// itself through this API, so the init-time registration pattern, the
// duplicate-overwrite semantics, the slog warning contract, and the
// concurrent-safety guarantee must all be locked down here.
//
// All stubs in this file are unexported and live in package tools_test so
// the registry is exercised through the same public surface real consumers
// see. Per AGENTS.md, every test runs t.Parallel() and uses testify/require
// for assertions.
package tools_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"sort"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/package-register/mocode/tools"
)

// ─── stubs ────────────────────────────────────────────────────────────────────
//
// stubTool and stubProvider implement the tools.Tool and tools.ToolProvider
// contracts from contracts.go. They are intentionally minimal: no fields
// beyond what each test needs. The id field on stubTool exists so tests can
// tell two otherwise-identical instances apart via pointer identity even
// when their Name()/Description()/Schema().Name are the same string.

// stubTool satisfies tools.Tool.
type stubTool struct {
	name string
}

func (s *stubTool) Name() string        { return s.name }
func (s *stubTool) Description() string { return s.name }
func (s *stubTool) Schema() tools.Schema {
	return tools.Schema{Name: s.name}
}

func (s *stubTool) Execute(_ context.Context, _ tools.ToolContext, _ json.RawMessage) (tools.ToolResult, error) {
	return tools.ToolResult{Content: "ok"}, nil
}

// stubProvider satisfies tools.ToolProvider.
type stubProvider struct {
	name  string
	tools []tools.Tool
}

func (p *stubProvider) Name() string { return p.name }
func (p *stubProvider) Tools() []tools.Tool {
	return p.tools
}

// initRegTool is the stub registered from init() to exercise the
// init-time registration path through the package-level Register
// forwarder. Its registered name ("init-tool") is unique enough that
// no other test in this file collides with it.
type initRegTool struct{}

func (i *initRegTool) Name() string        { return "init-tool" }
func (i *initRegTool) Description() string { return "registered from init()" }
func (i *initRegTool) Schema() tools.Schema {
	return tools.Schema{Name: "init-tool"}
}

func (i *initRegTool) Execute(_ context.Context, _ tools.ToolContext, _ json.RawMessage) (tools.ToolResult, error) {
	return tools.ToolResult{Content: "ok"}, nil
}

func init() {
	tools.Register("init-tool", &initRegTool{})
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// captureSlog replaces slog.Default() with a TextHandler writing into buf
// and returns a restore function the caller must defer. Tests use this to
// assert on the warnings produced by duplicate Register calls.
func captureSlog(t *testing.T, buf *bytes.Buffer) func() {
	t.Helper()
	oldDefault := slog.Default()
	handler := slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	slog.SetDefault(slog.New(handler))
	return func() { slog.SetDefault(oldDefault) }
}

// ─── NewRegistry + Register + Get ─────────────────────────────────────────────

// TestNewRegistry_Empty verifies that a freshly constructed Registry has
// no entries: Names() is empty (but non-nil) and All() is empty too.
func TestNewRegistry_Empty(t *testing.T) {
	t.Parallel()
	r := tools.NewRegistry()

	require.NotNil(t, r.Names(), "Names() must return a non-nil slice even when empty")
	require.Empty(t, r.Names())
	require.Empty(t, r.All())
}

// TestRegister_AndGet verifies the happy path: after Register, Get returns
// the same tool instance and the registered Name() round-trips correctly.
func TestRegister_AndGet(t *testing.T) {
	t.Parallel()
	r := tools.NewRegistry()
	want := &stubTool{name: "echo"}
	r.Register("echo", want)

	got, ok := r.Get("echo")
	require.True(t, ok)
	require.Same(t, want, got)
	require.Equal(t, "echo", got.Name())
}

// TestRegister_GetUnknown verifies that Get on a name that was never
// registered returns (nil, false) without panicking.
func TestRegister_GetUnknown(t *testing.T) {
	t.Parallel()
	r := tools.NewRegistry()

	got, ok := r.Get("nope")
	require.False(t, ok)
	require.Nil(t, got)
}

// TestRegister_DuplicateOverwritesAndWarns verifies two things at once:
// (1) a second Register with the same name replaces the prior tool
//
//	(observable via pointer identity), and
//
// (2) the overwrite emits a slog.Warn whose payload mentions the new
//
//	"tool registration" message, the WARN level, and the duplicate name.
func TestRegister_DuplicateOverwritesAndWarns(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	restore := captureSlog(t, buf)
	defer restore()

	r := tools.NewRegistry()
	first := &stubTool{name: "dup"}
	second := &stubTool{name: "dup"} // distinct *stubTool, identical Name()

	r.Register("dup", first)
	r.Register("dup", second)

	// Overwrite semantics: Get must return the second instance.
	got, ok := r.Get("dup")
	require.True(t, ok)
	require.Same(t, second, got, "second Register must overwrite the first")

	// slog semantics: a WARN record with the tool-registration message
	// and the duplicate name must have been emitted.
	out := buf.String()
	require.Contains(t, out, "WARN", "warning level must appear in log output")
	require.Contains(t, out, "tool registration", "warning message must reference tool registration")
	require.Contains(t, out, "dup", "warning payload must include the duplicate name")
}

// TestRegister_ConcurrentSafe hammers a single Registry with 100 concurrent
// Register goroutines (each inserting a unique name) and 100 concurrent
// Get goroutines (each reading one of those names). No goroutine must
// panic, the race detector (-race) must report no data races, and after
// every goroutine finishes, all 100 names must be present in the registry.
//
// We launch the Get goroutines in parallel with the Register goroutines
// to maximise lock contention. A Get that runs before its matching Register
// will legitimately observe (nil, false); that is not a failure of the
// registry. The post-wait assertion is the load-bearing one: the final
// state must reflect every Register.
func TestRegister_ConcurrentSafe(t *testing.T) {
	t.Parallel()
	const n = 100
	r := tools.NewRegistry()

	var wg sync.WaitGroup
	wg.Add(n + n)

	// Register phase: insert n tools with unique names.
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			name := toolName(i)
			r.Register(name, &stubTool{name: name})
		}(i)
	}

	// Get phase: query the same n names concurrently with the Register
	// phase. A Get that races ahead of its matching Register will see
	// (nil, false), which is fine; we only care that no Get panics and
	// that no race detector flag fires.
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			name := toolName(i)
			//nolint:errcheck // Get may legitimately return (nil, false)
			// during the race; we are only validating "no panic".
			_, _ = r.Get(name)
		}(i)
	}

	wg.Wait()

	// Final state: every name must be registered exactly once. A failed
	// Lock/Unlock inside Register would leave the map torn and either
	// panic during Register or surface here as a missing key.
	for i := 0; i < n; i++ {
		name := toolName(i)
		got, ok := r.Get(name)
		require.True(t, ok, "tool %q must be registered after the race", name)
		require.NotNil(t, got)
		require.Equal(t, name, got.Name())
	}
}

// toolName returns a deterministic, unique name for index i in the
// concurrent test. Fixed-width formatting keeps lexicographic order
// matching numeric order for any prefix-based assertions.
func toolName(i int) string {
	return "concurrent-" + leftPad3(i)
}

func leftPad3(i int) string {
	switch {
	case i < 10:
		return "00" + itoa(i)
	case i < 100:
		return "0" + itoa(i)
	default:
		return itoa(i)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	digits := "0123456789"
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = digits[i%10]
		i /= 10
	}
	return string(buf[pos:])
}

// TestNewRegistry_Isolated verifies that two Registry instances built
// from NewRegistry() are fully independent: a name registered on one
// does not appear on the other, and modifications on either do not
// leak into Default() (the package-level singleton).
func TestNewRegistry_Isolated(t *testing.T) {
	t.Parallel()
	r1 := tools.NewRegistry()
	r2 := tools.NewRegistry()

	r1.Register("alpha", &stubTool{name: "alpha"})
	r2.Register("alpha", &stubTool{name: "alpha"})

	// Each fresh registry carries its own entry.
	got1, ok1 := r1.Get("alpha")
	require.True(t, ok1)
	require.Equal(t, "alpha", got1.Name())

	got2, ok2 := r2.Get("alpha")
	require.True(t, ok2)
	require.Equal(t, "alpha", got2.Name())

	// The names registered on the fresh registries must not appear in
	// Default(). (Default may legitimately contain other tools registered
	// from other tests' init() functions; we only assert isolation from
	// the names we just registered here.)
	defNames := tools.Default().Names()
	require.NotContains(t, defNames, "alpha",
		"fresh-registered names must not leak into Default()")
}

// ─── RegisterProvider ─────────────────────────────────────────────────────────

// TestRegisterProvider_PopulatesAll verifies that every tool returned by
// a provider's Tools() is registered under the tool's own Name(). The
// provider's Name() is metadata only; it must not become a tool name.
func TestRegisterProvider_PopulatesAll(t *testing.T) {
	t.Parallel()
	r := tools.NewRegistry()

	provider := &stubProvider{
		name: "bundle-x",
		tools: []tools.Tool{
			&stubTool{name: "alpha"},
			&stubTool{name: "beta"},
			&stubTool{name: "gamma"},
		},
	}
	r.RegisterProvider(provider)

	for _, want := range []string{"alpha", "beta", "gamma"} {
		got, ok := r.Get(want)
		require.True(t, ok, "tool %q must be registered", want)
		require.Equal(t, want, got.Name())
	}

	// The provider's own name is NOT a tool name.
	_, ok := r.Get("bundle-x")
	require.False(t, ok, "provider name must not be registered as a tool")
}

// TestRegisterProvider_EmptyProvider verifies that a provider whose
// Tools() returns an empty (or nil) slice registers nothing and does
// not panic. This is the common case for an MCP server that exposes
// zero tools.
func TestRegisterProvider_EmptyProvider(t *testing.T) {
	t.Parallel()
	r := tools.NewRegistry()

	// Empty slice case.
	require.NotPanics(t, func() {
		r.RegisterProvider(&stubProvider{name: "empty", tools: []tools.Tool{}})
	})
	require.Empty(t, r.Names())

	// Nil slice case.
	require.NotPanics(t, func() {
		r.RegisterProvider(&stubProvider{name: "nil", tools: nil})
	})
	require.Empty(t, r.Names())
}

// ─── All / Names ──────────────────────────────────────────────────────────────

// TestAll_Snapshot verifies that two calls to All() return slices whose
// mutations do not bleed into each other, and that a tool registered
// after the first call is only visible in the second call's snapshot.
func TestAll_Snapshot(t *testing.T) {
	t.Parallel()
	r := tools.NewRegistry()
	r.Register("one", &stubTool{name: "one"})

	first := r.All()
	require.Len(t, first, 1)

	// Capture the registry state via a fresh snapshot before any
	// mutation, so we can compare against it after we mutate `first`.
	baseline := r.All()
	require.Len(t, baseline, 1)
	require.Equal(t, "one", baseline[0].Name())

	// Mutate the first snapshot heavily; the registry state must not
	// observe these mutations, and the freshly-returned snapshot must
	// still see the original tool.
	first[0] = nil
	afterMutation := r.All()
	require.Len(t, afterMutation, 1)
	require.NotNil(t, afterMutation[0],
		"registry state must not reflect snapshot mutations")
	require.Equal(t, "one", afterMutation[0].Name())

	// A subsequent Register must show up in the third snapshot but
	// must not retroactively appear in `first` (which still has length
	// 1 even though the registry now holds two tools).
	r.Register("two", &stubTool{name: "two"})
	third := r.All()
	require.Len(t, third, 2, "third snapshot must include the newly-registered tool")
	require.Len(t, first, 1, "first snapshot's length must not grow after the fact")
}

// TestAll_OrderUnspecified verifies that All() returns a slice with the
// right length but does not promise a particular order. Names() is the
// order-preserving accessor; All() is a snapshot for iteration.
func TestAll_OrderUnspecified(t *testing.T) {
	t.Parallel()
	r := tools.NewRegistry()
	r.Register("zebra", &stubTool{name: "zebra"})
	r.Register("apple", &stubTool{name: "apple"})
	r.Register("mango", &stubTool{name: "mango"})

	all := r.All()
	require.Len(t, all, 3)
	// No order assertion: just verify each registered name is present.
	seen := make(map[string]bool, len(all))
	for _, tool := range all {
		seen[tool.Name()] = true
	}
	require.True(t, seen["zebra"])
	require.True(t, seen["apple"])
	require.True(t, seen["mango"])
}

// TestNames_Sorted verifies that Names() returns all registered names in
// lexicographic (ascending) order. The sort is part of the Registry's
// contract so callers can rely on a stable ordering for diagnostics.
func TestNames_Sorted(t *testing.T) {
	t.Parallel()
	r := tools.NewRegistry()
	// Register out of order on purpose.
	r.Register("delta", &stubTool{name: "delta"})
	r.Register("alpha", &stubTool{name: "alpha"})
	r.Register("charlie", &stubTool{name: "charlie"})
	r.Register("bravo", &stubTool{name: "bravo"})

	got := r.Names()
	want := []string{"alpha", "bravo", "charlie", "delta"}
	require.Equal(t, want, got)
	// Defensive: also verify against sort.Strings just in case the
	// implementation drifts. Names() MUST be sorted.
	require.True(t, sort.StringsAreSorted(got),
		"Names() must return lexicographically sorted output")
}

// ─── Package-level loader (loader.go) ─────────────────────────────────────────

// TestLoader_PackageLevelForwarders verifies that the package-level
// Register / Get / Names / All functions are thin forwarders over
// defaultRegistry: a tool registered through tools.Register must be
// visible through every package-level accessor and through Default().
func TestLoader_PackageLevelForwarders(t *testing.T) {
	t.Parallel()

	const name = "loader-fwd-test"
	want := &stubTool{name: name}
	tools.Register(name, want)

	// Package-level forwarders must observe the registration.
	viaGet, ok := tools.Get(name)
	require.True(t, ok)
	require.Same(t, want, viaGet)

	require.Contains(t, tools.Names(), name)

	viaAll := tools.All()
	found := false
	for _, tool := range viaAll {
		if tool.Name() == name {
			found = true
			require.Same(t, want, tool)
			break
		}
	}
	require.True(t, found, "tools.All() must include %q", name)

	// Default() must return the same registry the forwarders wrap, so
	// the entry must be visible there too.
	viaDefault, ok := tools.Default().Get(name)
	require.True(t, ok)
	require.Same(t, want, viaDefault)
}

// TestLoader_DefaultReturnsSameInstance verifies that Default() is a
// singleton: every call returns the same *Registry pointer. This is
// required so callers can cache the pointer and still see registrations
// done from other packages' init() functions.
func TestLoader_DefaultReturnsSameInstance(t *testing.T) {
	t.Parallel()

	first := tools.Default()
	second := tools.Default()
	third := tools.Default()

	require.NotNil(t, first)
	require.Same(t, first, second)
	require.Same(t, second, third)
}

// TestLoader_InitTimeRegistration verifies the contract advertised in
// doc.go: tools register themselves from init() via tools.Register, and
// the agent runtime reads them out of Default() at startup. The stub
// initRegTool and its init() at the top of this file register
// "init-tool" against Default(); this test simply confirms that
// registration reached the registry before any test ran.
func TestLoader_InitTimeRegistration(t *testing.T) {
	t.Parallel()

	got, ok := tools.Get("init-tool")
	require.True(t, ok, "init() must have registered 'init-tool' before tests ran")
	require.NotNil(t, got)
	require.Equal(t, "init-tool", got.Name())

	// Default() must also expose the init-registered tool.
	defGot, defOk := tools.Default().Get("init-tool")
	require.True(t, defOk)
	require.Same(t, got, defGot)
}
