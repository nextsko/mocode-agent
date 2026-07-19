package agent

import (
	"encoding/json"

	"github.com/nextsko/mocode-agent/tools"
)

// agentToolContext implements tools.ToolContext for in-process tool
// invocations from the agent runtime.
//
// A fresh value is constructed per Execute call via NewToolContext. The
// returned cleanup func is currently a no-op; it is reserved for a future
// hook lifecycle and exists so callers don't have to be re-edited when
// per-call lifecycle support lands.
//
// This file is part of PR1 §1.7 (agent-side adapter skeleton) and is
// intentionally NOT wired to Coordinator — that wiring happens in PR3 §3.3.
// The file's sole job today is to compile against the toolkit contracts
// committed in PR1 §1.2 and to be ready for PR3 to construct.
//
// Design notes (see .superpowers/sdd/task-1.7-brief.md and
// docs/superpowers/plans/2026-06-29-agent-architecture-refactor.md §1.7):
//
//   - permission.Service (the existing project type) exposes
//     Request(ctx, opts) (bool, error), not Allow(name) bool. Wrapping it
//     directly would force the toolkit's PermissionChecker to carry a
//     context and full opts struct, which the toolkit intentionally keeps
//     minimal. We therefore accept a minimal permissionChecker interface
//     here; PR3 §3.3 will construct an adapter that bridges
//     permission.Service to this interface.
//
//   - toolutil exposes BeforeFunc/AfterFunc function types, not a Callbacks
//     struct with Before/After methods. We define a Callbacks struct here
//     that satisfies tools.ToolCallbacks; PR3 §3.3 will populate it from
//     the existing toolutil function types (or from AgentCallbacks, which
//     is the project-level lifecycle wrapper).
//
//   - mcp exposes GetStates() map[string]ClientInfo. We define mcpStates
//     and mcpState locally so PR1 §1.7 doesn't have to depend on the
//     concrete ClientInfo layout; PR2 will populate Tools on mcpState.
type agentToolContext struct {
	sessionID  string
	workingDir string
	perms      permissionChecker
	mcp        mcpStates
	cbs        *Callbacks
	deps       tools.RuntimeDependencies
}

// NewToolContext builds a fresh tool context for one Execute call. The
// returned cleanup func MUST be invoked by the caller after the tool
// completes; today it is a no-op, but it reserves the per-call lifecycle
// seam so PR3 (and future hook work) does not have to change call sites.
//
// Callers from PR3 §3.3 are expected to populate perms/mcp/cbs from the
// actual project types (permission.Service, mcp.GetStates(),
// toolutil.BeforeFunc/AfterFunc or AgentCallbacks).
func NewToolContext(
	sessionID string,
	workingDir string,
	perms permissionChecker,
	mcp mcpStates,
	cbs *Callbacks,
	runtimeDeps ...tools.RuntimeDependencies,
) (tools.ToolContext, func()) {
	var deps tools.RuntimeDependencies
	if len(runtimeDeps) > 0 {
		deps = runtimeDeps[0]
	}
	return &agentToolContext{
		sessionID:  sessionID,
		workingDir: workingDir,
		perms:      perms,
		mcp:        mcp,
		cbs:        cbs,
		deps:       deps,
	}, func() {}
}

// SessionID returns the agent session id this Execute call belongs to.
func (c *agentToolContext) SessionID() string { return c.sessionID }

// WorkingDir returns the working directory in which the tool is executing.
// Tools should treat this as authoritative; they MUST NOT fall back to
// os.Getwd.
func (c *agentToolContext) WorkingDir() string { return c.workingDir }

// Permissions returns the toolkit-shaped permission checker. The wrapper
// permAdapter just forwards Allow(name) to the underlying permissionChecker
// supplied to NewToolContext.
func (c *agentToolContext) Permissions() tools.PermissionChecker {
	if c.perms == nil {
		return permAdapter{}
	}
	return permAdapter{inner: c.perms}
}

// MCP returns the toolkit-shaped MCP handles. The wrapper mcpAdapter
// translates the local mcpState slice into []tools.MCPServerHandle.
func (c *agentToolContext) MCP() tools.MCPHandles {
	if c.mcp == nil {
		return mcpAdapter{}
	}
	return mcpAdapter{inner: c.mcp}
}

// Callbacks returns the toolkit-shaped before/after callbacks. The wrapper
// cbAdapter forwards Before/After to the underlying *Callbacks. A nil
// callbacks pointer is supported and degrades to a no-op adapter.
func (c *agentToolContext) Callbacks() tools.ToolCallbacks {
	if c.cbs == nil {
		return cbAdapter{}
	}
	return cbAdapter{inner: c.cbs}
}

func (c *agentToolContext) RuntimeDependencies() tools.RuntimeDependencies {
	return c.deps
}

// permissionChecker is the minimal contract the adapter needs from the
// permission layer. It mirrors tools.PermissionChecker (Allow(name) bool)
// deliberately so the toolkit interface stays the only shape callers have
// to know about.
//
// PR3 §3.3 is expected to provide a wrapper that satisfies this interface
// by delegating to permission.Service.Request — e.g. a closure that
// captures sessionID/workingDir and translates a name into the appropriate
// CreatePermissionRequest before calling Service.Request.
type permissionChecker interface {
	Allow(name string) bool
}

// mcpStates is the minimal interface the adapter needs from the MCP layer.
// A real implementation in PR2/PR3 is expected to wrap mcp.GetStates() and
// convert each ClientInfo into an mcpState. The Tools field is nil in PR1
// and is populated in PR2.
type mcpStates interface {
	Servers() []mcpState
}

// mcpState is a snapshot of one MCP server's connection state. Mirrors
// tools.MCPServerHandle's exported fields so mcpAdapter can copy them
// straight across without re-shaping.
type mcpState struct {
	Name  string
	State string       // "connected" | "disconnected" | "error" | "unknown"
	Tools []tools.Tool // nil in PR1; populated in PR2
}

// Callbacks is the agent-side wrapper around the toolkit's
// tools.ToolCallbacks contract. It exposes Before/After as methods (rather
// than as standalone function types like toolutil.BeforeFunc/AfterFunc) so
// that the toolkit interface is satisfied directly.
//
// PR3 §3.3 will populate Before/After from the existing toolutil
// BeforeFunc/AfterFunc chains (or from AgentCallbacks.RunAfterTool when
// only the after hook is needed).
type Callbacks struct {
	// Before is called before the inner tool executes. May be nil.
	Before func(name string, args json.RawMessage)
	// After is called after the inner tool returns. May be nil.
	After func(name string, result tools.ToolResult, err error)
}

// permAdapter wraps the local permissionChecker in the toolkit's
// tools.PermissionChecker interface. A zero value (no inner) is a no-op
// deny-all adapter, useful when the caller hasn't yet wired a permission
// service in (e.g. in tests).
type permAdapter struct {
	inner permissionChecker
}

// Allow returns true if the underlying permissionChecker permits the
// named tool call. A nil inner returns false.
func (p permAdapter) Allow(name string) bool {
	if p.inner == nil {
		return false
	}
	return p.inner.Allow(name)
}

// mcpAdapter wraps the local mcpStates in the toolkit's tools.MCPHandles
// interface. A zero value (no inner) returns nil from Servers.
type mcpAdapter struct {
	inner mcpStates
}

// Servers copies the local mcpStates into a fresh []tools.MCPServerHandle
// so callers cannot mutate the agent-side state through the toolkit
// interface.
func (m mcpAdapter) Servers() []tools.MCPServerHandle {
	if m.inner == nil {
		return nil
	}
	states := m.inner.Servers()
	if len(states) == 0 {
		return nil
	}
	out := make([]tools.MCPServerHandle, len(states))
	for i, s := range states {
		out[i] = tools.MCPServerHandle{
			Name:  s.Name,
			State: s.State,
			Tools: s.Tools,
		}
	}
	return out
}

// cbAdapter wraps *Callbacks in the toolkit's tools.ToolCallbacks
// interface. A nil inner degrades to a no-op adapter.
type cbAdapter struct {
	inner *Callbacks
}

// Before forwards to the wrapped *Callbacks. Safe on a nil inner or a nil
// Before field.
func (c cbAdapter) Before(name string, args json.RawMessage) {
	if c.inner == nil || c.inner.Before == nil {
		return
	}
	c.inner.Before(name, args)
}

// After forwards to the wrapped *Callbacks. Safe on a nil inner or a nil
// After field.
func (c cbAdapter) After(name string, result tools.ToolResult, err error) {
	if c.inner == nil || c.inner.After == nil {
		return
	}
	c.inner.After(name, result, err)
}
