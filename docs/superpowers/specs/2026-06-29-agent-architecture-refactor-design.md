---
comet_change: agent-architecture-refactor
role: technical-design
canonical_spec: openspec
---

# Design Doc: Agent Architecture Refactor

**Date**: 2026-06-29
**Change**: `agent-architecture-refactor`
**OpenSpec Context**: [proposal.md](../../openspec/changes/agent-architecture-refactor/proposal.md) · [design.md](../../openspec/changes/agent-architecture-refactor/design.md) · [tasks.md](../../openspec/changes/agent-architecture-refactor/tasks.md) · [tool-contracts spec](../../openspec/changes/agent-architecture-refactor/specs/tool-contracts/spec.md) · [agent-toolkit spec](../../openspec/changes/agent-architecture-refactor/specs/agent-toolkit/spec.md)

## 1. Context

`internal/core/agent/tools/` is the tool surface for the entire mocode runtime. It bundles:

- 10 built-in tools (`bash`, `edit`, `view`, `ls`, `write`, `multiedit`, `read_files`, `job_input`, `job_kill`, `job_output`).
- 30+ plugin tools (search, fetch, git, gitea, ssh, wechat, sourcegraph, todos, think, …).
- An MCP bridge.
- An LSP bridge.
- A capability/permission filter.
- An imperative `Registry` wired by hand from the agent package.

The agent package imports each tool package and the registry directly; `Coordinator` resolves a tool by name at LLM call time. There is no abstraction between the tool surface and the agent runtime — they are coupled at the import-graph level.

Two forces push for a refactor:

1. **Internal coupling.** Adding a new tool means editing the agent's registry wiring. Reusing a tool from a non-agent code path means re-walking the same dependencies. The surface is not a contract — it is a directory of files the agent happens to import.
2. **Future kernel swap.** The maintainer plans to either replace the LLM runtime with `trpc-agent-go` or rewrite the agent core from scratch. The toolkit surface is the largest single point of coupling between the agent and `charm.land/fantasy`; it must become framework-agnostic so the future kernel swap touches one adapter, not 40+ tools.

This design turns the tool surface into a separately importable, framework-agnostic toolkit that the agent runtime consumes through stable interfaces. Adding a tool is a one-line import; the agent core never imports specific tool subpackages.

## 2. Goals & Non-Goals

**Goals**

- Move all 40+ tools into `github.com/package-register/mocode/tools/` (top-level, same module).
- Define `Tool`, `ToolProvider`, `ToolContext`, `ToolResult`, `Schema`, `Capability` as the public contract. All toolkits implement `Tool`; related tools group behind a `ToolProvider`.
- Replace the imperative `Registry` with a declarative loader: each tool package calls `Register()` from its `init()`. The agent opts in via blank imports.
- Make the toolkit **framework-agnostic**: `tools/` does not import `charm.land/fantasy`, `internal/core/agent`, or `internal/core/config` types. All framework coupling lives in `internal/core/agent/agenttool_adapter.go`.
- Refactor `Coordinator` to depend on `*tools.Registry` and the `Tool` interface, not on specific tool subpackages.
- Preserve existing tool behavior and tests — this is a structural refactor, not a behavior change.

**Non-Goals**

- Runtime external tool discovery (WASM, Go plugin `.so`, subprocess transport, gRPC). Static in-process only.
- Agent-as-tool, multi-framework adapters, or any "tool that wraps an agent from another framework". The interface design accommodates this future work; the implementation does not include it.
- New tools, tool behavior changes, or capability additions.
- External versioning of the toolkit (no separate `go.mod`).
- Changes to `SessionAgent`, `evolution`, `evo`, `extension`, `roundtable`, `candidate`, `failover`, `notify`, `prompt`, `ctxcompress`.

## 3. Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│  external LLM runtime (charm.land/fantasy today; trpc-agent-go or   │
│  custom in the future)                                              │
│                              ▲                                      │
│                              │ []fantasy.AgentTool                  │
│                              │                                      │
│  internal/core/agent/         │                                      │
│  ┌────────────────────────────┴───────────────────────────┐          │
│  │  agenttool_adapter.go  (THE ONLY COUPLING POINT)      │          │
│  │      converts []tools.Tool → []fantasy.AgentTool      │          │
│  └────────────────────────────────────────────────────────┘          │
│                              ▲                                      │
│                              │ []Tool                                │
│                              │                                      │
│  ┌────────────────────────────┴───────────────────────────┐          │
│  │  Coordinator (consumes *tools.Registry, dispatches     │          │
│  │  by name, builds tool calls, applies permissions)     │          │
│  └────────────────────────────────────────────────────────┘          │
│                              ▲                                      │
│                              │ *tools.Registry, tools.ToolContext    │
│                              │                                      │
│  ┌────────────────────────────┴───────────────────────────┐          │
│  │  github.com/package-register/mocode/tools/             │          │
│  │  ├── contracts.go        (Tool, ToolProvider, ...)     │          │
│  │  ├── registry.go         (Default, Register, Get, ...)│          │
│  │  ├── loader.go           (init() pattern, idempotent)  │          │
│  │  ├── filter/             (capability/source filter)    │          │
│  │  ├── builtin/  (10 builtins, init() → Register)        │          │
│  │  ├── plugins/  (30+ plugins, init() → Register)        │          │
│  │  ├── mcp/      (MCP bridge as a ToolProvider)          │          │
│  │  └── lsp/      (LSP bridge as a ToolProvider)          │          │
│  │                                                       │          │
│  │  ZERO imports from: charm.land/fantasy,                │          │
│  │                     internal/core/agent,               │          │
│  │                     internal/core/config               │          │
│  └────────────────────────────────────────────────────────┘          │
│                                                                     │
│  internal/core/agent/  (not in toolkit)                             │
│  ├── agentic_fetch_tool.go   (agent-level workflow, stays here)    │
│  ├── roundtable_tool.go      (agent-level workflow, stays here)    │
│  ├── hooked_tool.go          (permission policy wrapper)           │
│  └── agentToolContext        (implements tools.ToolContext)        │
└─────────────────────────────────────────────────────────────────────┘
```

**Data flow** on an LLM tool call:

1. LLM emits a `tool_call` with `name` and `args`.
2. `Coordinator` looks up the tool: `tool, ok := registry.Get(name)`.
3. `Coordinator` consults `tctx.Permissions().Allow(name)`. If denied, returns a permission-denied `ToolResult`.
4. `Coordinator` calls `tool.Execute(ctx, tctx, args)`.
5. Tool returns `ToolResult`. If `Error != nil`, `Coordinator` formats the error and skips `Content`.
6. `Coordinator` returns the result to the LLM via the fantasy runtime.

## 4. Key Architectural Invariant: Toolkit Framework-Agnostic

This is the single most important constraint. It is the load-bearing principle for the future kernel swap.

**Rule.** The `github.com/package-register/mocode/tools/` package, including all its subpackages (`builtin/`, `plugins/`, `mcp/`, `lsp/`, `filter/`), MUST NOT import:

- `charm.land/fantasy` or any subpath thereof
- `charm.land/catwalk`
- `internal/core/agent` or any subpath
- `internal/core/config` or any subpath
- Any future LLM runtime library (trpc-agent-go, etc.)

**Enforcement.** CI runs `go list -deps ./tools/... | grep -E "(fantasy|catwalk|internal/core/(agent|config))"` and fails the build on any match. The check is added in PR1 and stays in `Taskfile.yaml` under `lint:deps` (or similar).

**Single coupling point.** All fantasy types are referenced from exactly one file: `internal/core/agent/agenttool_adapter.go`. This file is the only place a future kernel swap needs to edit. Its size is bounded (~50–100 lines) and its public API is `Adapter.ToFantasyTools(tools []Tool) []fantasy.AgentTool`.

## 5. Interface Specifications

### 5.1 `Tool` (the LLM-callable surface)

```go
package tools

// Tool is the contract every LLM-callable tool implements.
type Tool interface {
    // Name returns the tool's registered name. Must be unique within a Registry.
    Name() string
    // Description is the human-readable summary sent to the LLM.
    Description() string
    // Schema describes the tool's argument shape. Framework-agnostic.
    Schema() Schema
    // Execute runs the tool with the given args. ToolContext is request-scoped.
    // args is opaque JSON; the tool is responsible for unmarshaling.
    Execute(ctx context.Context, tctx ToolContext, args json.RawMessage) (ToolResult, error)
}
```

**Notes.** `args` is `json.RawMessage` rather than a typed struct so that the toolkit can be tested without the LLM runtime and so the same `Tool` can be adapted to multiple LLM providers. Tools unmarshal `args` into a domain type internally.

### 5.2 `ToolProvider` (a named bundle of tools)

```go
type ToolProvider interface {
    Name() string
    Tools() []Tool
}
```

A provider's `Tools()` may return a fresh slice each call (e.g. an MCP bridge reflecting live connections) or a stable slice (e.g. a static plugin).

### 5.3 `ToolContext` (request-scoped runtime handle)

```go
type ToolContext interface {
    SessionID() string
    WorkingDir() string
    Permissions() PermissionChecker
    MCP() MCPHandles
    Callbacks() ToolCallbacks
}

type PermissionChecker interface {
    Allow(name string) bool
}

type MCPHandles interface {
    Servers() []MCPServerHandle
}

type ToolCallbacks interface {
    Before(name string, args json.RawMessage)
    After(name string, result ToolResult, err error)
}
```

`ToolContext` is request-scoped: a fresh value is constructed for each `Execute` call. Implementations MUST NOT share mutable state across calls.

### 5.4 `Schema` (framework-agnostic tool description)

```go
type Schema struct {
    Name        string         // duplicate of Tool.Name() for serialization
    Description string         // duplicate of Tool.Description() for serialization
    Parameters  map[string]any // JSON Schema-shaped (type, properties, required, ...)
}
```

`Parameters` is `map[string]any` so the toolkit has no compile-time dependency on any JSON-Schema library. The adapter at `internal/core/agent/agenttool_adapter.go` converts `Schema.Parameters` into the LLM provider's expected shape.

### 5.5 `ToolResult` (structured output)

```go
type ToolResult struct {
    Content     string
    Error       error
    Attachments []message.Attachment
    Metadata    map[string]any
}
```

When `Error` is non-nil, `Content` MAY contain partial output; the agent runtime MUST surface the error to the LLM and not treat the partial content as success. The `Attachments` and `Metadata` fields are optional.

### 5.6 `Capability` (filter tag)

```go
type Capability string

// Tools advertise their capabilities via this method (default zero value = []).
type Capable interface {
    Capabilities() []Capability
}
```

`Capability` is a simple string type. Tools implement the `Capable` interface to advertise what they do; the filter layer uses this for enable/disable decisions.

## 6. Registry and Loader

### 6.1 Default Registry

```go
package tools

var defaultRegistry = NewRegistry()

func Register(name string, tool Tool)            { defaultRegistry.Register(name, tool) }
func RegisterProvider(p ToolProvider)            { defaultRegistry.RegisterProvider(p) }
func Get(name string) (Tool, bool)               { return defaultRegistry.Get(name) }
func Names() []string                            { return defaultRegistry.Names() }
func All() []Tool                                { return defaultRegistry.All() }
func Default() *Registry                         { return defaultRegistry }

type Registry struct {
    mu    sync.RWMutex
    tools map[string]Tool
    meta  map[string]sourceMeta
}

type sourceMeta struct {
    Source    string // "builtin", "plugin", "mcp", "lsp", ...
    Package   string // import path
    RegisteredAt time.Time
}

func NewRegistry() *Registry { ... }
func (r *Registry) Register(name string, tool Tool)
func (r *Registry) RegisterProvider(p ToolProvider)
func (r *Registry) Get(name string) (Tool, bool)
func (r *Registry) Names() []string
func (r *Registry) All() []Tool
```

### 6.2 init() pattern

Each tool subpackage ends with:

```go
package bash

import "github.com/package-register/mocode/tools"

func New() tools.Tool { return &bashTool{} }

func init() {
    tools.Register("bash", New())
}
```

A consumer opts in with a blank import:

```go
import (
    _ "github.com/package-register/mocode/tools/builtin/all"
    _ "github.com/package-register/mocode/tools/plugins/all"
)
```

`tools/builtin/all/all.go` and `tools/plugins/all/all.go` are umbrella files that blank-import every subpackage. Adding a new builtin = create a new subpackage + add one line to the umbrella.

### 6.3 Idempotency and Concurrency

- `Register` is guarded by a `sync.RWMutex`. `Get` takes a read lock.
- A duplicate name overwrites the previous entry and emits `slog.Warn` with the package path of both registrations.
- `init()` ordering is undefined in Go, but `Register` does not depend on call order — the last writer wins deterministically.
- The `meta` field records which package registered which name, so duplicate warnings can be precise.

## 7. Decisions

| # | Decision | Rationale | Alternatives considered |
|---|---|---|---|
| 1 | External tool / agent-as-tool deferred to a future change | The interface design (context/args/result three-segment) naturally accommodates a future `Transporter` wrapper; in-process scope keeps PR size reviewable | Implement external transport now (too large for one refactor); never add external (closed door) |
| 2 | `Schema` is a self-defined struct, not `fantasy.ToolSchema` | Future kernel swap (trpc-agent-go, custom) only needs to rewrite the adapter; tools remain intact | `fantasy.ToolSchema` (locks to current LLM runtime); `*jsonschema.Schema` (extra dep, awkward adapter) |
| 3 | `ToolContext` is pure in-process; no transport fields | YAGNI; future `ExternalToolContext` can implement the same interface without redefinition | Reserve `Transport` field in `ToolContext` (speculative, unused, complicates tests) |
| 4 | Permission checks live in `ToolContext.Permissions()` | The `Tool` interface stays clean; the policy decision stays in the agent | Registry middleware (extra layer, harder to reason about); per-tool self-check (policy scattered) |
| 5 | `agentic_fetch` / `roundtable` / `hooked_tool` stay in `internal/core/agent/` | They are agent-level workflows, not generic tools; moving them would force `tools/` to import the agent package, breaking the framework-agnostic invariant | Move them (breaks invariant); make `tools/` depend on agent (unacceptable) |
| 6 | Default singleton + `NewRegistry()` for custom | Default covers 99% of use cases; tests use isolated registries | Always-require-explicit-registry (verbose, no real benefit); always-singleton (no test isolation) |
| 7 | `agenttool_adapter.go` is the **only** coupling point between toolkit and LLM runtime | Makes future kernel swap a one-file change; CI enforces the boundary | Distribute fantasy imports across tool files (defeats the abstraction) |

## 8. Migration Plan

The 40+ file move lands across three PRs. Each PR keeps `go build ./...` and `go test ./...` green at the head of the branch.

### PR1 — Contracts + scaffold

- Add `tools/contracts.go`, `tools/registry.go`, `tools/loader.go`, `tools/filter/filter.go`.
- Add the `agentToolContext` adapter in `internal/core/agent/`.
- Add `tools/registry_test.go` and the dependency-boundary check in `Taskfile.yaml`.
- **No callsite changes yet.** Agent still imports old paths.
- The agent's `Coordinator` does not yet use the new registry.

### PR2 — Migrate implementations

- Move each builtin/plugin/MCP/LSP subpackage into `tools/...` per tasks.md §3–§5.
- Each subpackage's `init()` calls `tools.Register(...)` (or `RegisterProvider` for bridges).
- The legacy `internal/core/agent/tools/<name>/<name>.go` files become thin re-exports so the agent keeps building.
- `go test ./tools/...` is green at every sub-step.

### PR3 — Cutover + delete old

- `Coordinator` switches to `tools.Default()` (or an injected `*tools.Registry`).
- `agent_lifecycle.go` registers the agent's tool set via `_ ".../tools/builtin/all"` and `_ ".../tools/plugins/all"`.
- Delete the re-export files under `internal/core/agent/tools/`.
- Delete the entire `internal/core/agent/tools/` directory.
- Run the full post-change pipeline (`go build`, `go test ./...`, `task fmt`, `task lint:fix`).
- Update `AGENTS.md`, `docs/architecture/`, and `CONTRIBUTING.md` per tasks.md §7.

**Rollback.** Each PR is independently revertable. PR3 is the only one that deletes the old path; reverting PR3 restores the old layout with the new toolkit scaffolded beside it.

## 9. Testing Strategy

**Per-tool tests (preserve existing coverage).** Every existing `*_test.go` file moves with its tool to `tools/...`. Test assertions and behavior are unchanged. The migration is verified by `go test ./tools/...` matching the pre-refactor pass/fail status.

**Registry and loader tests (new).** In `tools/registry_test.go`:

- `init()`-time `Register` works after a blank import.
- Duplicate `Register` overwrites and logs a warning.
- Concurrent `Register` / `Get` is safe under `-race`.
- `NewRegistry()` is independent of `Default()`.
- `RegisterProvider` populates the registry from a `Tools()` slice.
- `Get` returns `false` for unknown names.

**Adapter tests (new).** In `internal/core/agent/agenttool_adapter_test.go`:

- `[]Tool → []fantasy.AgentTool` conversion preserves `Name`, `Description`, `Schema`, and call semantics.
- A `Tool` returning a `ToolResult{Error: ...}` is converted to a fantasy `ToolResult` that the runtime treats as a tool error.
- The adapter is the only file in `internal/core/agent/` that imports `charm.land/fantasy` types directly (verified by the boundary check).

**Dependency-boundary check (CI).** `go list -deps ./tools/... | grep -E '(fantasy|catwalk|internal/core/(agent|config))'` must produce no output. Fails the build otherwise.

**Integration smoke.** PR3 adds a smoke test in `internal/core/agent/coordinator_test.go` that runs a one-shot LLM call against a mock provider and verifies the tool is dispatched through the new registry and adapter.

## 10. Risks & Trade-offs

- **[Risk] 40+ files moved in two PRs is a large diff.** → Mitigation: PR2 migrates in sub-batches (builtins → plugins → bridges) and each sub-batch is independently testable. The diff is mechanical once the adapter in PR1 is in place.
- **[Risk] init() order may surprise a duplicate registration.** → Mitigation: idempotent `Register` with `slog.Warn` (matches `database/sql` convention). The `meta` field records the source package for precise warnings.
- **[Risk] Tests that previously reached into `internal/core/agent/tools/...` for fixtures break.** → Mitigation: PR2 moves test files with their tools; PR3 deletes the old path. External test code is not in scope for this repo.
- **[Risk] Future LLM runtime swap must touch the adapter only if the toolkit stays framework-agnostic.** → Mitigation: CI dependency-boundary check. If someone adds a `fantasy` import to a `tools/...` file, the check fails before review.
- **[Trade-off] agentic_fetch / roundtable / hooked_tool are not addressable via `tools.Default().Get()`.** This is by design (agent-level workflows). `Coordinator` injects them into the LLM's tool list directly. The price is "the registry doesn't see every tool the agent can call"; the benefit is the framework-agnostic invariant.
- **[Trade-off] The toolkit is not currently reusable outside the agent.** Without an external transport, no other process can consume it. This is the intended scope of this change. The interface design is the deliverable that makes future externalization cheap.

## 11. Open Questions / Future Work

These are explicitly out of scope for `agent-architecture-refactor` and are reserved for a future change.

1. **External tool runtime.** `SubprocessToolProvider` that spawns a child process and speaks a JSON-RPC dialect over stdio. Schema is exchanged at registration time. Pluggable `Transport` interface: stdio, HTTP, gRPC.
2. **Agent-as-tool.** A `SubagentTool` whose `Execute` invokes a child `SessionAgent` (or any object implementing a future `Agent` interface) and returns its result. Lets the LLM treat any agent as a single tool.
3. **Multi-framework adapters.** Adapters for `trpc-agent-go`, `langchaingo`, `autogen`, raw scripts. All route through the same `Tool` interface and `Registry`.
4. **`trpc-agent-go` kernel swap.** Replace `charm.land/fantasy` in `Coordinator` and `agenttool_adapter.go`. The toolkit surface is unchanged.
5. **External versioning.** Promote `tools/` to a separate Go module (own `go.mod`) once a second consumer exists.
6. **Tool marketplace.** A registry service that lists published tools and lets users install them as Go modules (replacing the blank-import opt-in).

## 12. References

- [proposal.md](../../openspec/changes/agent-architecture-refactor/proposal.md) — Why and What
- [design.md](../../openspec/changes/agent-architecture-refactor/design.md) — High-level architecture decisions
- [tasks.md](../../openspec/changes/agent-architecture-refactor/tasks.md) — Implementation checklist
- [tool-contracts spec](../../openspec/changes/agent-architecture-refactor/specs/tool-contracts/spec.md) — Interface requirements
- [agent-toolkit spec](../../openspec/changes/agent-architecture-refactor/specs/agent-toolkit/spec.md) — Toolkit requirements
- [brainstorm-summary.md](../../openspec/changes/agent-architecture-refactor/.comet/handoff/brainstorm-summary.md) — Brainstorming checkpoint
- [mocode AGENTS.md](../../../../AGENTS.md) — Project conventions
