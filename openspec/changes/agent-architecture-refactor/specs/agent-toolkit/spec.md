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

After the migration, every tool that existed under `internal/core/agent/tools/` before the refactor SHALL exist under `tools/...` after the refactor, with the same name and the same behavior. The old `internal/core/agent/tools/` directory SHALL be removed.

#### Scenario: No tool is silently dropped

- **WHEN** the migration is complete
- **THEN** the count of registered tool names in the new default registry matches the count of tool names in the pre-refactor registry

#### Scenario: No tool changes name

- **WHEN** the migration is complete
- **THEN** for every tool `T` that existed before, `tools.Default().Get("T")` returns a non-nil `Tool` after the cutover

#### Scenario: Old path is gone

- **WHEN** the migration is complete
- **THEN** `internal/core/agent/tools/` no longer exists in the repository

### Requirement: Agent consumes the new registry

`internal/core/agent/Coordinator` SHALL resolve tool calls through the new `*tools.Registry`. The agent package SHALL NOT import any path under `internal/core/agent/tools/`. Direct imports of moved tool subpackages by the agent are forbidden — the agent only imports `tools/contracts` and the umbrella `tools/builtin/all` plus `tools/plugins/all`.

#### Scenario: Agent does not import old tool paths

- **WHEN** `go build ./internal/core/agent/...` runs after the cutover
- **THEN** the build succeeds and `grep -r "internal/core/agent/tools" internal/core/agent/` returns no results

#### Scenario: Coordinator dispatches via the Tool interface

- **WHEN** the LLM emits a tool call with name `"bash"` and args `{"command": "ls"}`
- **THEN** the coordinator invokes `tools.Default().Get("bash").Execute(...)` and returns the resulting `ToolResult` to the LLM

### Requirement: Tests migrate with their tools

Each tool's test file SHALL move with its implementation into the new path. The new tests SHALL pass against the new `Tool` interface. The existing test coverage for each tool SHALL be preserved.

#### Scenario: Per-tool tests still pass

- **WHEN** `go test ./tools/builtin/bash/...` runs after the migration
- **THEN** the test suite for the bash tool reports the same pass/fail status as before the refactor

#### Scenario: New registry tests cover the loader

- **WHEN** `go test ./tools/...` runs after the migration
- **THEN** the registry and loader tests cover: init()-time registration, duplicate-name handling, custom registries, concurrent access, and the filter composition

### Requirement: One-line tool opt-in

Adding a new tool to a consumer's runtime SHALL require only a single blank-import line in the consumer's Go source. The agent SHALL NOT need to be edited to add a new tool to its default set.

#### Scenario: Adding a plugin requires one line

- **WHEN** a developer adds `_ "github.com/package-register/mocode/tools/plugins/foo"` to a consumer file
- **THEN** after `go build`, the consumer's registry includes the foo plugin's tools without any other source change

### Requirement: Fantasy adapter is the single LLM-runtime coupling point

The agent package SHALL define exactly one file — `agenttool_adapter.go` — that imports `charm.land/fantasy` and translates between `tools.Tool` and `fantasy.AgentTool`. No other file in `internal/core/agent/` SHALL import `charm.land/fantasy`. The adapter's public API SHALL be limited to `ToFantasyTools(tools []tools.Tool) []fantasy.AgentTool` and a per-tool execution wrapper.

#### Scenario: Only one file imports fantasy types

- **WHEN** a search is run for `charm.land/fantasy` across `internal/core/agent/`
- **THEN** exactly one Go file matches: `agenttool_adapter.go`

#### Scenario: Adapter translates Tool to fantasy.AgentTool

- **WHEN** the adapter receives `[]tools.Tool` containing a tool with `Schema{Name: "bash", Description: "...", Parameters: {...}}`
- **THEN** the returned `[]fantasy.AgentTool` contains a fantasy tool with the equivalent name, description, and schema fields

#### Scenario: Adapter executes a tool via the toolkit

- **WHEN** the LLM runtime invokes the adapter's `fantasy.AgentTool.Execute(...)` with arguments
- **THEN** the adapter unmarshals the arguments, calls `tools.Tool.Execute(ctx, tctx, args)`, and translates the returned `tools.ToolResult` back into the fantasy runtime's result type

#### Scenario: Kernel swap touches only the adapter

- **WHEN** the LLM runtime is replaced (e.g. `fantasy` swapped for `trpc-agent-go` in a future change)
- **THEN** the diff is limited to rewriting `agenttool_adapter.go`; no file under `tools/` requires modification

### Requirement: Coordinator dispatches through the adapter

The `Coordinator` SHALL resolve tool calls by name through `*tools.Registry.Get(...)` and SHALL pass the resulting `tools.Tool` to the adapter for execution. The `Coordinator` SHALL NOT call any `fantasy.AgentTool` method directly.

#### Scenario: Coordinator looks up tools by name

- **WHEN** the LLM emits a tool call with `name: "bash"`
- **THEN** the coordinator looks up `tools.Default().Get("bash")` and passes the result to the adapter

#### Scenario: Coordinator checks permissions before execution

- **WHEN** the coordinator is about to dispatch a tool call
- **THEN** it calls `tctx.Permissions().Allow(name)`; if the check returns false, the tool is NOT invoked and the tool message back to the LLM reports a permission-denied error

### Requirement: Adapter-mediated extension point

The adapter design SHALL leave a clean extension point so that future external tool sources (subprocess transport, agent-as-tool, gRPC) can be added without changing the coordinator or the LLM runtime. The extension point SHALL be a function or interface in the adapter file that wraps an arbitrary `tools.Tool` for execution, regardless of whether the tool is in-process or external.

#### Scenario: Adapter wraps any tools.Tool

- **WHEN** a future change adds a `SubprocessToolProvider` whose `Tools()` returns tools backed by an external process
- **THEN** the existing adapter transparently wraps those tools via the same `ToFantasyTools(...)` path without modification

#### Scenario: Adding external tool support does not touch the coordinator

- **WHEN** external tool support is added in a future change
- **THEN** the `Coordinator` source is unchanged; the adapter handles the new tool kinds
