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
