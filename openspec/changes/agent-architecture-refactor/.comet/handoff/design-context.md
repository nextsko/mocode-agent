# Comet Design Handoff

- Change: agent-architecture-refactor
- Phase: design
- Mode: compact
- Context hash: 3ef5dc0e23946e5516c78f9bbc82c270ca3ee3af77d73e24eebb340512daabe3

Generated-by: comet-handoff.sh

OpenSpec remains the canonical capability spec. This handoff is a deterministic, source-traceable context pack, not an agent-authored summary.

## openspec/changes/agent-architecture-refactor/proposal.md

- Source: openspec/changes/agent-architecture-refactor/proposal.md
- Lines: 1-34
- SHA256: 052ae18683900fe8add9de237b0c6fb953582a7652322bf9aa2e1828cb76307f

```md
## Why

`internal/core/agent/tools/` has grown into a 40+ tool monolith bundled into the agent package. Tool implementations depend directly on the agent's internal types (registry filters, permission checks, MCP/LSP plumbing), making the tool surface inseparable from the agent runtime and impossible to reuse outside the TUI/Coordinator path. We want the tool surface to be a reusable, contract-driven toolkit that the agent runtime consumes through stable interfaces, with new tools added without touching the agent core.

## What Changes

- Introduce a new top-level path `internal/tools/` (importable as `github.com/package-register/mocode/tools`) that owns tool contracts, registry, loader, and runtime context. The agent package becomes a consumer.
- Define interface contracts for `Tool` (the LLM-callable surface), `ToolProvider` (a named bundle of tools), `ToolContext` (request-scoped runtime handle), and `ToolResult` (the structured output). All 40+ tools implement these.
- Replace the current imperative `Registry` with a declarative loader: each tool package calls `Register()` at `init()` time; the agent core imports the tool packages via blank imports to opt in. Adding a tool becomes a one-line import.
- Move all 40+ existing tools (10 builtin + 30 plugins + mcp + lsp + filter + registry) into the new path under stable subpackages. The move is **one-shot** — no dual registration, no shim layer.
- Refactor `internal/core/agent/Coordinator` to consume the new `*tools.Registry` and dispatch tool calls through the new `Tool` interface. SessionAgent/extensions/evo/roundtable/candidate/failover are out of scope.

## Capabilities

### New Capabilities

- `tool-contracts`: interface abstractions (`Tool`, `ToolProvider`, `ToolContext`, `ToolResult`, `Capability`) plus the `Registry` and `init()`-based loader. Pure contract — zero business logic.
- `agent-toolkit`: the migrated implementations: builtin (bash, edit, view, ls, write, multiedit, read_files, job_input, job_kill, job_output), plugins (crawl, fetch, search, git, gitea, ssh, wechat, sourcegraph, todos, think, …), mcp bridge, lsp bridge, and the capability/permission filter.

### Modified Capabilities

(none — no existing capability spec currently exists)

## Impact

- Affected code:
  - **New**: `internal/tools/` (full new package)
  - **Moved**: `internal/core/agent/tools/{builtin,mcp,lsp,plugins,filter,registry.go,tools.go,compat.go,fetch_types.go,mcp-tools.go,transfer.go}` → `internal/tools/{builtin,mcp,lsp,plugins,filter,registry,context,result}`
  - **Modified**: `internal/core/agent/coordinator.go`, `internal/core/agent/agent.go`, `internal/core/agent/agent_lifecycle.go`, `internal/core/agent/hooked_tool.go`, `internal/core/agent/roundtable_tool.go`, `internal/core/agent/agentic_fetch_tool.go` — all switch to the new `*tools.Registry` and `tools.Tool` interface.
- **BREAKING**: any external code importing the old paths under `internal/core/agent/tools/...` must update imports. (Internal to this repo.)
- **BREAKING**: tool authors adding new tools in this repo must use the new `Register()` pattern; the previous direct append-to-Registry pattern is removed.
- Dependencies: none new. Uses existing `charm.land/fantasy` types where they apply.
- Test impact: existing per-tool tests migrate with their files; registry/loader tests are rewritten against the new contract.
- Out of scope: `internal/core/agent/extension`, `evo`, `evolution`, `roundtable`, `candidate`, `failover`, `notify`, `prompt`, `ctxcompress`, `SessionAgent` interface, `Coordinator` business logic beyond tool dispatch.
```

## openspec/changes/agent-architecture-refactor/design.md

- Source: openspec/changes/agent-architecture-refactor/design.md
- Lines: 1-156
- SHA256: 173d9673d3264d2bf4e72cbf7187d292624ccd048c8f3b31cc70fa1c30259a30

[TRUNCATED]

```md
## Context

`internal/core/agent/tools/` is the tool surface for the entire mocode runtime. It currently contains:

- 10 builtin tools (bash, edit, view, write, ls, multiedit, read_files, job_input, job_kill, job_output) under `tools/builtin/`.
- 30+ plugin tools under `tools/plugins/` (crawl, fetch, search, git, gitea, ssh, wechat, sourcegraph, todos, think, …).
- An MCP bridge under `tools/mcp/`.
- An LSP bridge under `tools/lsp/`.
- A capability/permission filter under `tools/filter/`.
- An imperative `Registry` (`tools/registry.go`) that the agent package wires up by hand.

The agent package (`internal/core/agent/`) imports each tool package and the registry directly, then the `Coordinator` resolves a tool by name from the registry when the LLM emits a tool call. There is no abstraction between "the tool surface" and "the agent runtime" — they are coupled at the import-graph level.

**Problem.** Adding a new tool means editing the agent's registry wiring, and reusing a tool from a non-agent code path (an admin handler, a CLI subcommand, a future Gateway bot) means re-walking the same dependencies. The surface is not a contract, it's a directory of files the agent happens to import.

**Goal of this change.** Make the tool surface a separately importable, contract-driven toolkit that the agent runtime consumes through stable interfaces. Adding a tool is a one-line import; the agent core never knows about specific tools.

## Goals / Non-Goals

**Goals**

- Move all 40+ tools into a top-level `tools/` package (`github.com/package-register/mocode/tools`) that lives in the same Go module but is conceptually a third-party toolkit.
- Define `Tool`, `ToolProvider`, `ToolContext`, `ToolResult`, `Capability` as the public contract. Every tool implements `Tool`; related tools group behind a `ToolProvider`.
- Replace the imperative `Registry` with a declarative loader: each tool package calls `Register()` from its `init()`. The agent core opts in via blank imports.
- Refactor `Coordinator` (and the few other agent-package consumers) to depend on `*tools.Registry` and the `Tool` interface, not on specific tool subpackages.
- Keep existing tool behavior and tests intact — this is a structural refactor, not a behavior change.

**Non-Goals**

- No changes to `SessionAgent`, `evolution`, `evo`, `extension`, `roundtable`, `candidate`, `failover`, `notify`, `prompt`, `ctxcompress`. Out of scope by user decision.
- No runtime plugin discovery (WASM, Go plugin `.so`, dynamic Go modules). Static import only.
- No new tools and no tool behavior changes.
- No external-versioning story (no separate `go.mod` for the toolkit, no semver).

## Decisions

### 1. Package path: top-level `tools/`, same module

`github.com/package-register/mocode/tools` (top-level, not `internal/tools`).

- **Rationale**: matches the "3rd-party package" intent and leaves the door open to publishing. `internal/` would lock out any external consumer. Same module keeps the build simple (no `go.mod` split, no replace directives).
- **Rejected**: `internal/tools/` (locks out external use), separate `go.mod` for the toolkit (overhead of version management for an in-repo refactor).

### 2. Interface contracts at the root of the toolkit

`Tool`, `ToolProvider`, `ToolContext`, `ToolResult`, `Capability` live in `tools/contracts.go` (or a small `contract/` subpackage). Zero business logic. The `Tool` interface is the only one tools must implement directly; `ToolContext` is provided to `Execute` as a request-scoped handle.

```go
type Tool interface {
    Name() string
    Description() string
    Schema() fantasy.ToolSchema      // re-exported where needed
    Execute(ctx context.Context, tctx ToolContext, args json.RawMessage) (ToolResult, error)
}

type ToolProvider interface {
    Name() string
    Tools() []Tool
}

type ToolContext interface {
    SessionID() string
    WorkingDir() string
    Permissions() PermissionChecker
    MCP() MCPHandles
    Callbacks() ToolCallbacks
    // future: Logger, Metrics, ...
}

type ToolResult struct {
    Content     string
    Error       error
    Attachments []message.Attachment
    Metadata    map[string]any
}
```

- **Rationale**: separating `ToolContext` from `Tool` keeps the interface minimal and lets tests pass a fake context without standing up an agent.
- **Rejected**: a single fat `Toolkit` interface (too coarse), making `ToolContext` a struct (loses test substitution).

```

Full source: openspec/changes/agent-architecture-refactor/design.md

## openspec/changes/agent-architecture-refactor/tasks.md

- Source: openspec/changes/agent-architecture-refactor/tasks.md
- Lines: 1-76
- SHA256: 18f9b1d50316d44ad7722805185a9b5d1b5e7847727059ee1fb2e8f94fcc398b

```md
## 1. Scaffold the toolkit package

- [ ] 1.1 Create `tools/` directory at module root with `doc.go` describing the toolkit's purpose and import policy
- [ ] 1.2 Add `tools/contracts.go` defining `Tool`, `ToolProvider`, `ToolContext`, `ToolResult`, `Schema`, `Capability`, `PermissionChecker`, `MCPHandles`, `ToolCallbacks`, and `Attachment` types per the `tool-contracts` spec
- [ ] 1.3 Add `tools/registry.go` with the default `Registry` singleton, `Register`, `RegisterProvider`, `Get`, `Names`, `All`, and a constructor `NewRegistry`
- [ ] 1.4 Add `tools/loader.go` (or extend registry) implementing the init-time `Register` pattern with duplicate-name warning via `slog.Warn`
- [ ] 1.5 Add `tools/filter/filter.go` (moved from `internal/core/agent/tools/filter/filter.go`) wired to the new `Capability` and registry types
- [ ] 1.6 Add `tools/registry_test.go` covering init-time registration, duplicate-name behavior, concurrent `Register`/`Get`, and `NewRegistry` isolation
- [ ] 1.7 Verify `go build ./tools/...` and `go test ./tools/...` pass with no other consumers yet

## 2. Build the agent-side adapter

- [ ] 2.1 Add `agentToolContext` struct in `internal/core/agent/` that implements `tools.ToolContext` by wrapping the existing helpers (`*toolutil.Callbacks`, permission checker, MCP handles, working dir, session id)
- [ ] 2.2 Add a `NewToolContext(...)` constructor on the agent package that builds the adapter from the existing agent wiring
- [ ] 2.3 Add a test that constructs a fake `tools.ToolContext` and verifies the agent adapter exposes the expected values (session id, working dir, permissions, callbacks, MCP handles)

## 3. Move built-in tools

- [ ] 3.1 Create `tools/builtin/bash/` and move `bash.go`, `safe.go`, `bash.tpl`, and tests; add `init()` calling `tools.Register`
- [ ] 3.2 Create `tools/builtin/edit/` and move `edit.go` and `edit.md`; add `init()`
- [ ] 3.3 Create `tools/builtin/view/` and move `view.go` and `view.md`; add `init()`
- [ ] 3.4 Create `tools/builtin/ls/` and move `ls.go` and `ls.md`; add `init()`
- [ ] 3.5 Create `tools/builtin/write/` and move `write.go` and `write.md`; add `init()`
- [ ] 3.6 Create `tools/builtin/multiedit/` and move `multiedit.go` and `multiedit.md`; add `init()`
- [ ] 3.7 Create `tools/builtin/read_files/` and move `read_files.go` and `read_files.md`; add `init()`
- [ ] 3.8 Create `tools/builtin/job_input/`, `tools/builtin/job_kill/`, `tools/builtin/job_output/` and move their files; add `init()` to each
- [ ] 3.9 Create `tools/builtin/all/all.go` that blank-imports every builtin subpackage; verify `go build ./tools/builtin/all/...` registers all 10
- [ ] 3.10 Update the legacy `internal/core/agent/tools/builtin/<name>/<name>.go` to re-export from the new path so the agent still builds during the cutover window
- [ ] 3.11 Run `go test ./tools/builtin/...` and confirm every builtin test passes

## 4. Move plugin tools

- [ ] 4.1 Move `tools/plugins/think/` (single tool); add `init()`
- [ ] 4.2 Move `tools/plugins/todos/`; add `init()`
- [ ] 4.3 Move `tools/plugins/glob/` and `tools/plugins/grep/` (search common); add `init()`
- [ ] 4.4 Move `tools/plugins/web_search/` and `tools/plugins/web_fetch/`; add `init()`
- [ ] 4.5 Move `tools/plugins/crawl/`, `tools/plugins/fetch/`, `tools/plugins/download/`, `tools/plugins/download_docs/`; add `init()`
- [ ] 4.6 Move `tools/plugins/ssh_list_hosts/`, `tools/plugins/ssh_exec/`, `tools/plugins/ssh_download/`, `tools/plugins/ssh_upload/` (reuse the `sshcommon` helpers); add `init()` to each
- [ ] 4.7 Move `tools/plugins/git_plan_commits/`, `tools/plugins/git_execute_commits/` (reuse `gitopscommon`); add `init()`
- [ ] 4.8 Move `tools/plugins/gitea_issues/`, `tools/plugins/gitea_pulls/`, `tools/plugins/gitea_notifications/` (reuse `giteacommon`); add `init()`
- [ ] 4.9 Move `tools/plugins/sourcegraph/`; add `init()`
- [ ] 4.10 Move `tools/plugins/send_wechat_file/`, `tools/plugins/send_wechat_image/`, `tools/plugins/screenshot_to_wechat/`, `tools/plugins/message_export/`; add `init()`
- [ ] 4.11 Move `tools/plugins/session_export/`, `tools/plugins/session_search/`, `tools/plugins/session_summary/`; add `init()`
- [ ] 4.12 Move `tools/plugins/mocode_info/`, `tools/plugins/mocode_logs/`; add `init()`
- [ ] 4.13 Move `tools/plugins/lsp_restart/`; add `init()`
- [ ] 4.14 Move `tools/plugins/list_mcp_resources/`, `tools/plugins/read_mcp_resource/`; add `init()`
- [ ] 4.15 Create `tools/plugins/all/all.go` blank-importing every plugin subpackage; verify `go build ./tools/plugins/all/...` registers all plugins
- [ ] 4.16 Update the legacy `internal/core/agent/tools/plugins/<name>/<name>.go` files to re-export from the new paths
- [ ] 4.17 Run `go test ./tools/plugins/...` and confirm plugin tests pass

## 5. Move MCP and LSP bridges

- [ ] 5.1 Move `tools/mcp/` (init, prompts, resources, tools, `mcp-tools.go`) and adapt the package to expose a `ToolProvider` whose `Tools()` reflects `GetStates()`
- [ ] 5.2 Move `tools/lsp/` (client, handlers, manager, util) and adapt the bridge to lazily resolve the LSP client via `ToolContext`
- [ ] 5.3 Update the agent package's `hooked_tool.go`, `agentic_fetch_tool.go`, and `roundtable_tool.go` to use the new bridges
- [ ] 5.4 Update the legacy `internal/core/agent/tools/{mcp,lsp}/` paths to re-export from the new paths
- [ ] 5.5 Run `go test ./tools/mcp/... ./tools/lsp/...` and confirm bridge tests pass

## 6. Cutover the agent to the new registry

- [ ] 6.1 Update `internal/core/agent/Coordinator` to use `tools.Default()` (or a `*tools.Registry` injected at construction) for tool lookup
- [ ] 6.2 Update `internal/core/agent/agent.go` and `internal/core/agent/agent_lifecycle.go` to register the agent's tool set via `_ "github.com/package-register/mocode/tools/builtin/all"` and `_ "github.com/package-register/mocode/tools/plugins/all"`
- [ ] 6.3 Remove the per-tool imports from `internal/core/agent/`; verify `grep -r "internal/core/agent/tools" internal/core/agent/` returns nothing
- [ ] 6.4 Delete the re-export files under `internal/core/agent/tools/builtin/` and `internal/core/agent/tools/plugins/`
- [ ] 6.5 Delete `internal/core/agent/tools/` entirely (registry.go, tools.go, compat.go, fetch_types.go, transfer.go, mcp-tools.go, plus all subdirs)
- [ ] 6.6 Run `go build ./...` and `go test ./...`; fix any residual import errors

## 7. Tests, lint, docs

- [ ] 7.1 Add a new `tools/builtin/all/all_test.go` and `tools/plugins/all/all_test.go` that asserts every advertised name is registered (no missing inits)
- [ ] 7.2 Run `task fmt`, `task lint:fix`; resolve any new violations
- [ ] 7.3 Update `AGENTS.md` to describe the new tool layout and the one-line-opt-in rule for contributors
- [ ] 7.4 Update `docs/architecture/` (or add a new doc) with a short page on the toolkit contract and how to add a new tool
- [ ] 7.5 Update `CONTRIBUTING.md` with a "How to add a new tool" recipe (blank import the new subpackage from the appropriate `all/` umbrella)
- [ ] 7.6 Run the full post-change local pipeline: `go build`, `go test ./...`, `task fmt`, `task lint:fix`
- [ ] 7.7 Commit per logical group (contracts, agent adapter, builtin, plugins, mcp/lsp, cutover, docs) and tag the cutover commit
```

## openspec/changes/agent-architecture-refactor/specs/agent-toolkit/spec.md

- Source: openspec/changes/agent-architecture-refactor/specs/agent-toolkit/spec.md
- Lines: 1-186
- SHA256: f58adcd95b47737a4cc9c64c52f59f7e83a43dd49ca475a44e81e6a359ab0d75

[TRUNCATED]

```md
## ADDED Requirements

### Requirement: Built-in toolset

The toolkit SHALL ship the existing 10 built-in tools under `tools/builtin/`. Each tool SHALL implement the `Tool` interface from the `tool-contracts` capability and SHALL register itself with the default registry via `init()`.

The built-in toolset SHALL include, at minimum: `bash`, `edit`, `view`, `ls`, `write`, `multiedit`, `read_files`, `job_input`, `job_kill`, `job_output`.

#### Scenario: All builtins registered at import

- **WHEN** a consumer blank-imports `github.com/package-register/mocode/tools/builtin/all`
- **THEN** `tools.Default().Names()` contains every built-in tool name

#### Scenario: Builtins match prior behavior

- **WHEN** a built-in tool is invoked with the same arguments it received before the refactor
- **THEN** the tool produces the same `ToolResult` content and side effects as the pre-refactor implementation

### Requirement: Plugin toolset

The toolkit SHALL ship the existing 30+ plugin tools under `tools/plugins/`. Each plugin SHALL implement the `Tool` interface and SHALL register itself at `init()`. Plugins SHALL be grouped under stable subpackages (one per logical plugin family) so consumers can opt in selectively.

#### Scenario: Plugins opt in via blank import

- **WHEN** a consumer blank-imports a single plugin subpackage (e.g. `tools/plugins/fetch`)
- **THEN** only that plugin's tools appear in the default registry, not other plugins'

#### Scenario: Plugin umbrella blank import registers all

- **WHEN** a consumer blank-imports `github.com/package-register/mocode/tools/plugins/all`
- **THEN** all plugin tools are registered with the default registry

#### Scenario: Plugin behavior is preserved

- **WHEN** a plugin tool is invoked with the same arguments it received before the refactor
- **THEN** the tool produces the same `ToolResult` content and side effects as the pre-refactor implementation

### Requirement: MCP bridge

The toolkit SHALL provide an MCP bridge under `tools/mcp/` that exposes connected MCP servers' tools as `Tool` implementations. The bridge SHALL be a `ToolProvider` whose `Tools()` returns a fresh slice each call, reflecting the currently-connected MCP servers.

#### Scenario: MCP bridge reflects live connections

- **WHEN** an MCP server connects after the toolkit is initialized
- **THEN** a subsequent call to the MCP bridge's `Tools()` includes the newly connected server's tools

#### Scenario: MCP bridge skips disconnected servers

- **WHEN** an MCP server is in the `disconnected` state
- **THEN** the MCP bridge's `Tools()` does NOT include tools from that server

### Requirement: LSP bridge

The toolkit SHALL provide an LSP bridge under `tools/lsp/` that exposes LSP-backed tools (e.g. `lsp_restart`, LSP-driven code actions). The bridge SHALL lazily resolve the LSP client from the `ToolContext` and SHALL NOT hold a long-lived reference to a client at registration time.

#### Scenario: LSP tool resolves client at call time

- **WHEN** an LSP-backed tool is invoked and the `ToolContext` has a live LSP client
- **THEN** the tool uses the live client to fulfill the request

#### Scenario: LSP tool returns graceful error without client

- **WHEN** an LSP-backed tool is invoked and the `ToolContext` has no LSP client
- **THEN** the tool returns a `ToolResult{Error: ...}` describing the missing client, not a panic

### Requirement: Permission and capability filter

The toolkit SHALL expose the existing `tools/filter` package under its new path. The filter SHALL compose with the `ToolContext.Permissions()` check so the agent can gate tool execution at call time, not just at registration time.

#### Scenario: Filter narrows the registry

- **WHEN** the agent constructs a filtered registry view with `filter.ByCapability("fs.read")`
- **THEN** the resulting view contains only tools advertising that capability

#### Scenario: Runtime permission check

- **WHEN** a tool whose name requires elevated permission is invoked and `ToolContext.Permissions().Allow(name)` returns false
- **THEN** the agent runtime SHALL refuse the call and surface a permission-denied `ToolResult`

### Requirement: Migration parity
```

Full source: openspec/changes/agent-architecture-refactor/specs/agent-toolkit/spec.md

## openspec/changes/agent-architecture-refactor/specs/tool-contracts/spec.md

- Source: openspec/changes/agent-architecture-refactor/specs/tool-contracts/spec.md
- Lines: 1-179
- SHA256: 620663eb74a0e003b048c1e6c12c67e3314e136a186821e723840ef637114089

[TRUNCATED]

```md
## ADDED Requirements

### Requirement: Tool interface contract

The toolkit SHALL define a `Tool` interface that every tool implementation satisfies. The interface SHALL expose: `Name() string`, `Description() string`, `Schema() Schema`, and `Execute(ctx context.Context, tctx ToolContext, args json.RawMessage) (ToolResult, error)`.

The `Tool` interface SHALL NOT depend on any package outside the toolkit (no agent-package types, no coordinator types, no session-store types).

#### Scenario: Minimal Tool implementation compiles

- **WHEN** a developer writes a struct that implements all four `Tool` methods
- **THEN** the struct satisfies the `Tool` interface and can be passed to `Register(name, tool)`

#### Scenario: Tool has no transitive agent-package dependency

- **WHEN** a developer imports only `github.com/package-register/mocode/tools` to use `Tool`
- **THEN** the import succeeds without requiring `internal/core/agent/...` packages

### Requirement: ToolProvider interface contract

The toolkit SHALL define a `ToolProvider` interface for grouping related tools behind a single named bundle. The interface SHALL expose `Name() string` and `Tools() []Tool`.

A tool MAY be registered directly OR through a `ToolProvider`. The registry SHALL accept both forms.

#### Scenario: Provider registers multiple tools

- **WHEN** a developer calls `RegisterProvider(p)` where `p.Tools()` returns three `Tool` implementations
- **THEN** the registry contains all three tools, each addressable by its own `Name()`

#### Scenario: Single tool and provider can coexist

- **WHEN** a developer calls `Register(name, tool)` and `RegisterProvider(provider)` in the same process
- **THEN** the registry contains both the direct tool and every tool exposed by the provider

### Requirement: ToolContext request-scoped handle

The toolkit SHALL define a `ToolContext` interface that tools receive on each `Execute` call. The interface SHALL expose: `SessionID() string`, `WorkingDir() string`, `Permissions() PermissionChecker`, `MCP() MCPHandles`, and `Callbacks() ToolCallbacks`.

`ToolContext` SHALL be request-scoped — a fresh value is constructed for each `Execute` call. Implementations MUST NOT share mutable state across calls.

#### Scenario: ToolContext exposes session id

- **WHEN** a tool calls `tctx.SessionID()` during `Execute`
- **THEN** it returns the id of the session that initiated the tool call

#### Scenario: ToolContext is fresh per call

- **WHEN** the agent calls `tool.Execute(ctx, tctx1, args)` and later `tool.Execute(ctx, tctx2, args)` for the same tool in the same session
- **THEN** `tctx1` and `tctx2` are distinct values (they may carry the same session id but are not the same pointer)

#### Scenario: ToolContext is substitutable in tests

- **WHEN** a test author writes a struct that implements all `ToolContext` methods
- **THEN** the struct satisfies `ToolContext` and can be passed to `Execute` without standing up an agent

### Requirement: ToolResult structured output

The toolkit SHALL define a `ToolResult` struct returned by `Execute`. The struct SHALL expose: `Content string`, `Error error`, `Attachments []Attachment`, and `Metadata map[string]any`.

`Error` non-nil SHALL take precedence over `Content` for LLM consumption. The agent runtime SHALL format the error message and suppress `Content` when `Error` is set.

#### Scenario: Error result is treated as failure

- **WHEN** a tool returns a `ToolResult{Error: errors.New("boom"), Content: "partial"}`
- **THEN** the agent runtime surfaces the error to the LLM and does NOT include the partial content as success output

#### Scenario: Successful result carries content

- **WHEN** a tool returns `ToolResult{Content: "ok"}` with `Error: nil`
- **THEN** the agent runtime includes the content in the tool message back to the LLM

### Requirement: Capability tag

The toolkit SHALL define a `Capability` type (string-typed) for declaring what a tool can do. Tools SHALL advertise their capabilities via a `Capabilities() []Capability` method on `Tool` (default empty). The filter layer SHALL use capabilities to enable or disable tools per session.

#### Scenario: Capability advertises what the tool does

- **WHEN** a tool's `Capabilities()` returns `[]Capability{"fs.read", "fs.write"}`
- **THEN** the registry can be filtered to include or exclude the tool based on these tags

```

Full source: openspec/changes/agent-architecture-refactor/specs/tool-contracts/spec.md

