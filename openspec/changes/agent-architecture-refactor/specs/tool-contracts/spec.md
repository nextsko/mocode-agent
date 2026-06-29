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

### Requirement: Default Registry with init() loader

The toolkit SHALL provide a default `Registry` accessible as a package-level singleton. The default registry SHALL expose: `Register(name string, tool Tool)`, `RegisterProvider(p ToolProvider)`, `Get(name string) (Tool, bool)`, `Names() []string`, and `All() []Tool`.

`Register` and `RegisterProvider` SHALL be safe for concurrent use and SHALL be safe to call from `init()`. A duplicate name SHALL overwrite the previous entry and emit a `slog.Warn` log line.

#### Scenario: init()-time registration works

- **WHEN** a tool package's `init()` calls `tools.Register("bash", bashTool)`
- **THEN** after the package is imported, `tools.Default().Get("bash")` returns `bashTool`

#### Scenario: Duplicate name is logged and overwrites

- **WHEN** two packages both call `Register("bash", tool)` at init
- **THEN** the second call wins, the first tool is no longer reachable by name, and a warning is logged

#### Scenario: Registry is concurrent-safe

- **WHEN** two goroutines call `Register` and `Get` concurrently
- **THEN** both calls succeed and `Get` returns a non-nil tool for the registered name

### Requirement: Custom Registry for isolation

The toolkit SHALL allow callers to create an isolated `NewRegistry() *Registry` for testing or for non-default tool sets. The agent runtime MAY use the default registry or a custom one.

#### Scenario: Custom registry is independent of default

- **WHEN** a test creates a `NewRegistry()` and registers a tool on it
- **THEN** the default registry does not contain that tool

### Requirement: Tool filter

The toolkit SHALL provide a `Filter` that takes a registry and a predicate, returning a new registry view containing only the tools matching the predicate. The filter SHALL compose with capability, source, and category predicates.

#### Scenario: Filter by capability

- **WHEN** a caller filters with `Filter.WithCapabilities("fs.read")`
- **THEN** the resulting view contains only tools that advertise `"fs.read"` in their `Capabilities()`

#### Scenario: Filter by source

- **WHEN** a caller filters by source `"builtin"`
- **THEN** the resulting view contains only tools whose source is `"builtin"`

### Requirement: Schema type

The toolkit SHALL define a `Schema` type that describes a tool's argument shape. The Schema SHALL be JSON-serializable for LLM tool-call consumption. Each tool's `Schema()` SHALL return a non-nil schema describing the expected `args` payload to `Execute`.

#### Scenario: Schema is JSON-serializable

- **WHEN** a tool returns a `Schema` from `Schema()`
- **THEN** `json.Marshal(schema)` produces a valid JSON object describing the tool's parameters

#### Scenario: Empty schema is allowed

- **WHEN** a tool takes no arguments and returns `Schema{}`
- **THEN** the tool is still registered and `Execute` receives a zero-length `json.RawMessage`

### Requirement: Schema is framework-agnostic

The `Schema` type SHALL be defined in the toolkit's own package and SHALL NOT reference any LLM runtime library type (notably `charm.land/fantasy`, `charm.land/catwalk`, or any future LLM provider). The toolkit's `Schema` SHALL be the only schema type tools implement. Conversion to a specific LLM runtime's schema type SHALL be performed by an adapter in the agent package.

#### Scenario: Schema struct has no fantasy import

- **WHEN** a developer inspects `tools/contracts.go` (or wherever `Schema` is defined)
- **THEN** the file's import block does NOT include `charm.land/fantasy` or any other LLM provider package

#### Scenario: Schema can be adapted to a provider's schema type

- **WHEN** the agent package's adapter receives a `tools.Schema`
- **THEN** the adapter converts it to the LLM runtime's expected schema shape without modifying the toolkit

### Requirement: Toolkit has zero LLM runtime dependency

The `github.com/package-register/mocode/tools/` package, including all its subpackages (`builtin/`, `plugins/`, `mcp/`, `lsp/`, `filter/`), SHALL NOT import `charm.land/fantasy`, `charm.land/catwalk`, `internal/core/agent`, or `internal/core/config`. A CI-level dependency check SHALL enforce this constraint.

#### Scenario: Dependency check passes for the toolkit

- **WHEN** CI runs `go list -deps ./tools/... | grep -E "(fantasy|catwalk|internal/core/(agent|config))"`
- **THEN** the command produces no output and the build is green

#### Scenario: Adding a fantasy import to tools fails CI

- **WHEN** a developer adds an import to `charm.land/fantasy` in any file under `tools/`
- **THEN** the dependency check fails and the PR cannot merge

### Requirement: Registry holds no LLM provider handle

The default `Registry` and any `Registry` instance SHALL NOT store a reference to any LLM runtime (`fantasy.LanguageModel`, `fantasy.Agent`, or equivalents). The registry is a name → `Tool` map; the agent runtime owns the LLM runtime.

#### Scenario: Registry data structure

- **WHEN** a developer inspects the `Registry` struct
- **THEN** its fields are limited to the tool map and registration metadata; no field references a `fantasy.LanguageModel` or provider type

#### Scenario: Registry is usable without a model

- **WHEN** `tools.Default()` is called in a test that has no LLM runtime configured
- **THEN** the registry works normally and tools can be registered and retrieved
