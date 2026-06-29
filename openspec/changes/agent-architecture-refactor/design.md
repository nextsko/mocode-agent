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

### 3. Loader: explicit `Register()` at `init()` against a default registry

Each tool subpackage (e.g. `tools/builtin/bash/bash.go`) ends with:

```go
func init() { tools.Register(builtin.New()) }
```

The `Registry` is a package-level default in `tools/registry.go`. A consumer (e.g. the agent) imports the tool packages it wants:

```go
import (
    _ "github.com/package-register/mocode/tools/builtin/bash"
    _ "github.com/package-register/mocode/tools/builtin/edit"
    // ...
)
```

`Register()` is safe under a mutex and idempotent (last write wins on duplicate name, with a `slog.Warn`).

- **Rationale**: explicit `Register()` at `init()` is the standard Go pattern (`database/sql` drivers, `image` decoders, `expvar`). It is debuggable, testable, and cross-platform.
- **Rejected**: implicit registry init in a central package (init order is undefined), per-instance `Register(r *Registry)` with hand-wired registries (loses the "one-line to add a tool" property), `plugin.Open(".so")` (Linux-only, hard to debug).

### 4. ToolContext adapter bridges old and new during the cutover

The agent currently passes ad-hoc helpers (`*toolutil.Callbacks`, `permission.Checker`, MCP handles) to tool `Execute` methods. During the one-shot migration, a single `agentToolContext` struct in the agent package implements the new `ToolContext` interface and wraps the existing helpers. After the cutover the agent's helpers can be removed in follow-up commits.

- **Rationale**: this is the only place the migration is non-mechanical. Isolating it in one adapter makes the cutover auditable.
- **Rejected**: keeping the old helpers in the new `tools` package (couples the toolkit back to the agent).

### 5. Migration: one-shot, dependency-ordered, in three PRs

The 40+ tool files move in a single branch but the cutover lands in three PRs to keep `go build ./...` green at every step:

1. **PR1 — Contracts + scaffold**: introduce `tools/` with interfaces, registry, loader, and the `agentToolContext` adapter. Agent still imports old paths.
2. **PR2 — Migrate implementations**: move builtin + plugins + mcp + lsp + filter into `tools/...`, one subpackage at a time. Each subpackage's `init()` registers itself. Old paths re-export from the new paths so the agent keeps building.
3. **PR3 — Cutover + delete old**: switch the agent to the new registry, delete the re-exports and the old `internal/core/agent/tools/` directory.

The user agreed to "one-shot migration" — this three-PR sequence is the same one-shot cutover expressed in reviewable chunks. The toolkit is unusable from outside the agent until PR3 lands.

- **Rationale**: a single 4k-line diff is unreviewable; three PRs each have a single mechanical job and a green `go build` at every step.
- **Rejected**: dual registration (user explicitly chose one-shot), per-tool flag-day (untestable).

### 6. Filter and capability model stays inside `tools/filter/`

The existing `tools/filter/Filter` (by source / category / capability) moves with the rest and keeps its public API. The agent's permission checks compose on top via the `ToolContext.Permissions()` handle.

- **Rationale**: the filter is already a self-contained piece; moving it preserves a small surface for the agent to call.

## Risks / Trade-offs

- **Init-order coupling.** Two tools registering with the same name is order-dependent. → Mitigation: `Register()` is idempotent, namespaced names are encouraged (`bash.run`, `git.commit`).
- **Test churn.** ~40+ tool test files move packages. → Mitigation: tests migrate with their files; CI matrix catches regressions.
- **Bulk import rewrite.** Every `internal/core/agent/tools/...` import in the agent changes. → Mitigation: `gofmt -r` or scripted sed for the bulk rename, then hand-fix the few non-mechanical spots; verify with `go build ./...` after every PR.
- **`internal/` constraint for new tools.** Future contributors may want to drop a new tool under `internal/core/agent/`. → Mitigation: document the rule in `tools/README.md` and the `AgentSkills` / `CONTRIBUTING.md` guides.
- **No external-versioning story.** The toolkit's API can break across commits in the same module. → Mitigation: defer versioning to the day the toolkit is actually published; tag-breaking changes in commit messages.
- **LSP and MCP bridges have lifecycle state.** They are not just functions — they hold running clients. → Mitigation: `ToolProvider` for these returns tools that lazily resolve clients via the `ToolContext`; the client lifecycle stays in the agent's wiring.

## Migration Plan

1. Create `tools/` with contracts, registry, loader, and the `agentToolContext` adapter. **No callsite changes yet.**
2. Add the new subpackages with the moved files (builtin, plugins, mcp, lsp, filter) and their `init()`-time `Register()` calls. Old paths keep re-exporting.
3. Run `go build ./...`, `go test ./...`. Fix any non-mechanical import / signature issues.
4. Switch `Coordinator` and the few other agent consumers to the new registry. Delete the re-exports.
5. Delete `internal/core/agent/tools/` and any now-unused helpers in `internal/core/agent/agent_helpers.go`.
6. Run the full pipeline: `go build`, `go test`, `task lint:fix`, `task fmt`.
7. Update `AGENTS.md` and `docs/architecture/` to reflect the new layout.

**Rollback.** Each PR is independently revertable. PR3 (the cutover) is the only one that deletes the old path; reverting PR3 restores the old layout with the new toolkit scaffolded beside it.

## Open Questions

- Should `Tool.Schema()` return a `fantasy.ToolSchema` (a re-export), a `*jsonschema.Schema`, or our own `Schema` struct? Decide in PR1.
- Should the `ToolContext` carry a logger / metrics handle from day one, or stub them? Stub for now; add in a follow-up.
- The `agentic_fetch` and `roundtable` tools in the agent package are not in `tools/` — are they part of the toolkit or stay in the agent? **Assumption**: stay in the agent; they are agent-level workflows, not generic tools. Confirm in PR1.
- The `hooked_tool.go` wrapper in the agent package — does it become a `ToolProvider` middleware in `tools/`, or stay in the agent? **Assumption**: stay in the agent (it's permission-policy wiring). Confirm in PR1.
