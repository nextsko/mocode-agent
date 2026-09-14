# Mocode vs trpc-agent-go — Tool System Comparison Matrix

> Generated from source analysis of `mocode` (`charm.land/fantasy` kernel) and
> `trpc-agent-go@v0.8.0` (`trpc.group/trpc-go/trpc-agent-go/tool`).

---

## 1. Core Interface Architecture

| Dimension | mocode | trpc-agent-go | Key Difference |
|---|---|---|---|
| **Base interface** | `fantasy.AgentTool` — `Info() ToolInfo`, `Run(ctx, ToolCall) (ToolResponse, error)` | `tool.Tool` — `Declaration() *Declaration` | mocode combines info+execution; trpc separates declaration from calling |
| **Callable variant** | Same interface (always callable) | `tool.CallableTool` adds `Call(ctx, jsonArgs []byte) (any, error)` | trpc has a separate callable sub-interface |
| **Streaming** | Job-based (bash → background job with `job_output`/`job_input`/`job_kill`) | `tool.StreamableTool` — `StreamableCall(ctx, jsonArgs) (*StreamReader, error)` | trpc has native bidirectional streaming; mocode uses OS-process jobs |
| **Schema type** | `fantasy.ToolInfo` — `Name`, `Description`, `Parameters map[string]any`, `Required []string` | `tool.Declaration` — `Name`, `Description`, `InputSchema *Schema`, `OutputSchema *Schema` | trpc carries typed `*Schema` for both input **and** output; mocode uses untyped `map[string]any` for input only |
| **Schema generation** | Hand-written `json` + `description` struct tags | `function.NewFunctionTool` auto-generates from Go types via reflection + `jsonschema` tags | trpc is reflection-driven; mocode is manual |
| **Tool grouping** | `ToolPlugin` interface — `Descriptors() []ToolDescriptor` + `Build(ctx, deps) []AgentTool` | `tool.ToolSet` interface — `Tools(ctx) []Tool` + `Close() error` + `Name() string` | mocode has richer metadata (Kind, Category); trpc ToolSet has lifecycle (Close) and naming |
| **Context passing** | Custom context keys: `SessionIDContextKey`, `MessageIDContextKey`, `SupportsImagesKey`, `ModelNameKey` | `ContextKeyToolCallID` injected into stdlib context | mocode passes session/model metadata via context; trpc only passes tool-call ID |

---

## 2. Tool-by-Tool Mapping

### 2.1 File Operations

| mocode Tool | trpc Equivalent | ABI Differences | Parameter Passing | Error Handling | Adapter Notes |
|---|---|---|---|---|---|
| **`view`** | `read_file` | mocode: returns `ViewResponseMetadata` with line numbers, renders images; trpc: returns `readFileResponse{BaseDirectory, FileName, Contents, Message}` | mocode `ViewParams{File, Offset, Limit}`; trpc `readFileRequest{FileName, StartLine, NumLines}` | mocode: `fantasy.ToolResponse` (text/image/media); trpc: Go struct + `error` | Parameter rename: `file_path→file_name`, `offset→start_line`, `limit→num_lines`; trpc has no image rendering |
| **`read_files`** | `read_multiple_files` | mocode: explicit file list; trpc: glob patterns with case-sensitivity flag | mocode `ReadFilesParams{Paths []string}`; trpc `readMultipleFilesRequest{Patterns []string, CaseSensitive bool}` | mocode: per-file `ReadFilesResult`; trpc: per-file `fileReadResult` with message | trpc accepts glob patterns not explicit paths — adapter needs glob expansion or path→pattern conversion |
| **`ls`** | `list_file` | mocode: recursive tree view with depth control, `.gitignore`-aware; trpc: flat directory listing, files/folders separated | mocode `LSParams{Path, Depth, Ignore}`; trpc `listFileRequest{Path}` | Both return structured error on non-directory | trpc has no recursive depth or ignore patterns — significant feature gap |
| **`write`** | `save_file` | mocode: permission-gated, history-tracked, LSP-integrated; trpc: simple write with overwrite flag | mocode `WriteParams{Path, Content}`; trpc `saveFileRequest{FileName, Contents, Overwrite bool}` | mocode: permission request → approval flow; trpc: direct `os.WriteFile` | Must add permission layer and history tracking in adapter |
| **`edit`** | `replace_content` | mocode: LSP-integrated, line-based edits with old/new string matching; trpc: simple string replacement with `num_replacements` | mocode `EditParams{Path, OldStr, NewStr}`; trpc `replaceContentRequest{FileName, OldString, NewString, NumReplacements}` | mocode: tracks file history, validates via LSP diagnostics; trpc: returns replacement count | mocode `edit` has implicit LSP validation; trpc has no LSP integration |
| **`multiedit`** | N/A — trpc specific | mocode: batch multiple edit operations in one call with per-op error tracking; trpc has no batch edit | mocode `MultiEditParams{Path, Operations []MultiEditOperation}` | mocode: `FailedEdit` list for partial failures | Must implement as repeated `replace_content` calls or build a batch wrapper |
| **N/A — mocode specific** | `search_file` | — | — | — | trpc glob-based file search within a directory; mocode has `glob` (different category) |
| **`glob`** *(search)* | `search_file` | mocode: sorted by mtime, respects `.gitignore`, max 100 results; trpc: doublestar glob with case-sensitivity | mocode `GlobParams{Pattern}`; trpc `searchFileRequest{Path, Pattern, CaseSensitive}` | mocode: file list only; trpc: separate files/folders lists | Different glob engines: mocode uses `filepath.Glob`; trpc uses `doublestar.Glob` |

### 2.2 Search Operations

| mocode Tool | trpc Equivalent | ABI Differences | Parameter Passing | Error Handling | Adapter Notes |
|---|---|---|---|---|---|
| **`grep`** | `search_content` | mocode: ripgrep-backed, regex/literal, include/exclude filters, max_results cap; trpc: Go regexp with concurrent file scanning | mocode `GrepParams{Pattern, Path, Include, Exclude, LiteralText, MaxResults}`; trpc `searchContentRequest{Path, FilePattern, FileCaseSensitive, ContentPattern, ContentCaseSensitive}` | mocode: `GrepMatch` array with line numbers; trpc: `fileMatch→lineMatch` nested structure | trpc uses built-in Go regex (no ripgrep) — performance difference on large codebases |
| **`sourcegraph`** | N/A — mocode specific | External Sourcegraph API integration | `SourcegraphParams{Query, Count}` | — | No trpc counterpart; adapter must keep mocode implementation |

### 2.3 Execution / Process Tools

| mocode Tool | trpc Equivalent | ABI Differences | Parameter Passing | Error Handling | Adapter Notes |
|---|---|---|---|---|---|
| **`bash`** | N/A — mocode specific | Full shell with permission system, safe-command detection, background jobs, attribution | `BashParams{Command, Timeout}` | Permission request → approval; `BashResponseMetadata` with exit code, stdout, stderr | trpc has no built-in shell tool; `skill_run` is the closest (sandboxed code execution) |
| **`job_output`** | N/A — mocode specific | Read output from background bash jobs | `JobOutputParams{JobID}` | — | trpc streaming model eliminates need for job polling |
| **`job_input`** | N/A — mocode specific | Send stdin to running background jobs | `JobInputParams{JobID, Input}` | — | — |
| **`job_kill`** | N/A — mocode specific | Kill background jobs | `JobKillParams{JobID}` | — | — |

### 2.4 Network / Web Tools

| mocode Tool | trpc Equivalent | ABI Differences | Parameter Passing | Error Handling | Adapter Notes |
|---|---|---|---|---|---|
| **`fetch`** | `webfetch/httpfetch` (client-side) | mocode: URL fetch + markdown conversion with retry; trpc: HTTP fetch with URL filtering | `FetchParams{URL}`; trpc: HTTP fetch with domain filters | mocode: permission-gated, retry-wrapped; trpc: direct fetch | trpc also has `geminifetch`/`claudefetch` for server-side fetching |
| **`web_fetch`** | N/A — mocode specific | Higher-level fetch with content extraction | `WebFetchParams{URL}` | — | — |
| **`web_search`** | `duckduckgo_search` | mocode: provider-abstracted web search; trpc: DuckDuckGo Instant Answer API only | mocode `WebSearchParams{Query}`; trpc `searchRequest{Query}` | mocode: provider interface (`netcommon.Provider`); trpc: direct API call | trpc DuckDuckGo is factual/encyclopedic only — not a general web search |
| **`crawl`** | N/A — mocode specific | Recursive web crawling with depth/page limits | `CrawlParams{URL, MaxDepth, MaxPages}` | — | No trpc counterpart |
| **`download`** | N/A — mocode specific | File download with permission gating | `DownloadParams{URL, Path}` | — | — |
| **`download_docs`** | N/A — mocode specific | GitHub repo doc download | `DownloadDocsParams{RepoURL, DocsPath}` | — | — |

### 2.5 Agent Coordination

| mocode Tool | trpc Equivalent | ABI Differences | Parameter Passing | Error Handling | Adapter Notes |
|---|---|---|---|---|---|
| **`transfer_to_agent`** | `transfer_to_agent` | **Nearly identical semantics.** mocode: `fantasy.AgentTool` with `TransferController` + `TransferTracker` (loop detection); trpc: `CallableTool` with `agent.Invocation` context + `TransferInfo` | Both: `{agent_name, message}`. mocode adds sliding-window loop detection (`windowSize`, `minUnique`) | mocode: `TransferController.OnTransfer()` can reject; trpc: sets `invocation.TransferInfo`, framework handles handoff | Core logic is compatible. mocode has extra `TransferTracker` for repetition detection borrowed from trpc's `SwarmConfig` |
| **N/A — mocode specific** | `agent.Tool` (agent-as-tool) | trpc wraps any `agent.Agent` as a callable tool with `HistoryScope`, `StreamInner`; mocode uses Coordinator pattern | trpc: `agent.NewTool(agent, WithHistoryScope(...), WithStreamInner(...))` | — | mocode doesn't have an "agent-as-tool" wrapper — uses coordinator/sub-agent pattern instead |

### 2.6 LSP / Code Intelligence

| mocode Tool | trpc Equivalent | ABI Differences | Parameter Passing | Error Handling | Adapter Notes |
|---|---|---|---|---|---|
| **`diagnostics`** | N/A — mocode specific | LSP diagnostics (errors, warnings) | `DiagnosticsParams{Path}` | — | — |
| **`references`** | N/A — mocode specific | LSP find-references | `ReferencesParams{Path, Line, Column}` | — | — |
| **`lsp_restart`** | N/A — mocode specific | Restart LSP servers | `LSPRestartParams{Language}` | — | — |

### 2.7 Session / State Management

| mocode Tool | trpc Equivalent | ABI Differences | Parameter Passing | Error Handling | Adapter Notes |
|---|---|---|---|---|---|
| **`todos`** | N/A — mocode specific | Persistent todo list with priorities and status | `TodosParams{Action, Items}` | — | — |
| **`session_export`** | N/A — mocode specific | Export session messages to file | `SessionExportParams{Path}` | — | — |
| **`message_export`** | N/A — mocode specific | Export individual messages | `MessageExportParams{MessageID}` | — | — |
| **`session_summary`** | N/A — mocode specific | Summarize session with scheduling | `SessionSummaryParams{}` | — | — |
| **`session_search`** | N/A — mocode specific | Search across sessions | `SessionSearchParams{Query}` | — | — |

### 2.8 MCP Integration

| mocode Tool | trpc Equivalent | ABI Differences | Parameter Passing | Error Handling | Adapter Notes |
|---|---|---|---|---|---|
| **`mcp_{name}_{tool}`** (dynamic) | `mcp.ToolSet` | mocode: dynamic tool names prefixed `mcp_{server}_{tool}`, permission-gated, Docker whitelist; trpc: `mcpToolSet` with `Init()`, reconnect logic, singleflight | mocode: `mcp.RunTool(ctx, cfg, mcpName, toolName, input)`; trpc: `mcpTool.Call(ctx, jsonArgs)` → `sessionManager.callTool()` | mocode: permission service + image/media type detection; trpc: reconnect on `session_expired`, `broken_pipe`, etc. | trpc has richer MCP lifecycle (reconnect, singleflight); mocode has per-call permission gating |
| **`list_mcp_resources`** | N/A — mocode specific | List MCP server resources | — | — | — |
| **`read_mcp_resource`** | N/A — mocode specific | Read specific MCP resource | — | — | — |

### 2.9 Memory / Knowledge

| mocode Tool | trpc Equivalent |
|---|---|
| **`memory_add/update/delete/clear/search/load`** | N/A — mocode specific (via `memory.Service.Tools()`) |

### 2.10 Reasoning / Meta

| mocode Tool | trpc Equivalent |
|---|---|
| **`think`** | N/A — mocode specific (structured reasoning scratchpad) |
| **`mocode_info`** | N/A — mocode specific (environment introspection) |
| **`mocode_logs`** | N/A — mocode specific |
| **`references`** (LSP) | N/A — mocode specific |

### 2.11 Gitea / GitOps

| mocode Tool | trpc Equivalent |
|---|---|
| **`gitea_issues`**, **`gitea_pulls`**, **`gitea_notifications`** | N/A — mocode specific |
| **`git_plan_commits`**, **`git_execute_commits`** | N/A — mocode specific |

### 2.12 SSH Tools

| mocode Tool | trpc Equivalent |
|---|---|
| **`ssh_exec`**, **`ssh_upload`**, **`ssh_download`**, **`ssh_list_hosts`** | N/A — mocode specific |

### 2.13 WeChat Integration

| mocode Tool | trpc Equivalent |
|---|---|
| **`send_wechat_image`**, **`send_wechat_file`**, **`screenshot_to_wechat`** | N/A — mocode specific |

### 2.14 trpc-only Tools (no mocode counterpart)

| trpc Tool | Description | mocode Workaround |
|---|---|---|
| **`skill_run`** | Execute skill scripts in sandboxed workspace with code executor | Use `bash` tool with manual sandboxing |
| **`skill_load`** | Load skill definitions | `learn` tool authors/refines SKILL.md (`create`/`refine`/`list`); loading stays prompt-driven via `<available_skills>` + `view` |
| **`skill_list_docs`** | List skill documentation | N/A |
| **`skill_select_docs`** | Select skill docs for context | N/A |
| **`agent.Tool`** | Wrap any agent as a callable tool | Coordinator/sub-agent pattern |
| **`webfetch/geminifetch`** | Server-side fetch via Gemini API | N/A — client-side only |
| **`webfetch/claudefetch`** | Server-side fetch via Claude API | N/A — reserved for future |
| **`tool.Merge`** | Generic result merging for parallel tool execution | Not needed (no parallel tool execution) |

---

## 3. Cross-Cutting Concerns

### 3.1 Callback / Hook System

| Feature | mocode | trpc-agent-go |
|---|---|---|
| **Before-tool hook** | Permission service intercepts before execution (`permission.Request`) | `Callbacks.RegisterBeforeTool()` — can modify context, arguments, or short-circuit |
| **After-tool hook** | Extension manager, file tracker | `Callbacks.RegisterAfterTool()` — can modify result or context |
| **Result-to-messages** | N/A | `Callbacks.RegisterToolResultMessages()` — custom tool response formatting |
| **Callback chaining** | Single permission gate | Multiple callbacks with `continueOnError` / `continueOnResponse` options |
| **Structured args** | Permission request struct | `BeforeToolArgs{ToolName, Declaration, Arguments}` / `AfterToolArgs{...}` |

### 3.2 Permission / Security Model

| Dimension | mocode | trpc-agent-go |
|---|---|---|
| **Permission model** | Explicit per-call approval via `permission.Service.Request()` | No built-in permission system |
| **Path safety** | Working directory enforcement + `.gitignore` | `resolvePath()` blocks absolute paths and `..` traversal |
| **File size limits** | Implicit via tool implementation | `maxFileSize` option (default 1MB) |
| **Docker whitelist** | `whitelistDockerTools` for MCP tools | N/A |
| **Safe commands** | `bash/safe.go` classifies safe vs dangerous commands | N/A — no shell tool |

### 3.3 Error Handling Patterns

| Pattern | mocode | trpc-agent-go |
|---|---|---|
| **Soft errors** | `fantasy.NewTextErrorResponse(msg)` — returned as tool response, no Go error | Embedded in response struct `Message` field + returned as Go `error` |
| **Permission denied** | `NewPermissionDeniedResponse()` with `StopTurn` | N/A — no permission system |
| **Hard errors** | Go `error` return from `Run()` | Go `error` return from `Call()` |
| **Retry** | `toolutil.WithRetry()` wrapper on network tools | N/A — caller responsibility |
| **Image support check** | `GetSupportsImagesFromContext()` → error if unsupported | N/A — no image handling in tools |

### 3.4 Registry / Discovery

| Feature | mocode | trpc-agent-go |
|---|---|---|
| **Plugin architecture** | `ToolPlugin` interface with `Descriptors()` + `Build()` | `ToolSet` interface with `Tools()` + `Close()` |
| **Tool metadata** | `ToolDescriptor{Name, Kind, Category}` | `Declaration{Name, Description, InputSchema, OutputSchema}` |
| **Category system** | 14 categories (file, exec, search, network, lsp, mocode, session, mcp_meta, memory, reasoning, gitea, gitops, ssh) | None — flat tool list |
| **Kind system** | `builtin` vs `plugin` | None |
| **Dynamic registration** | MCP tools discovered at runtime | MCP tools discovered via `listTools()` |
| **Filtering** | `filter.Apply()` with `IncludeNames`, `ExcludeNames`, `ByCategory`, `ByKind` | `FilterTools()` with `FilterFunc`, `NewIncludeToolNamesFilter`, `NewExcludeToolNamesFilter` |

---

## 4. Adapter Requirements for Kernel Swap

When swapping mocode from `charm.land/fantasy` to `trpc-agent-go`, the following
adaptations are needed:

### 4.1 Interface Adapter (`agenttool_adapter.go`)

```
fantasy.AgentTool  →  trpc tool.Tool + tool.CallableTool
┌──────────────────┐    ┌─────────────────────────┐
│ Info() ToolInfo  │ →  │ Declaration() *Declaration │
│ Run(ctx, call)   │ →  │ Call(ctx, jsonArgs) (any, error) │
│   ToolResponse   │    │   raw return value         │
└──────────────────┘    └─────────────────────────┘
```

| Adapter Task | Complexity | Notes |
|---|---|---|
| `ToolInfo` → `Declaration` | Low | Map `Name`, `Description`; convert `Parameters map[string]any` to `*Schema` |
| `ToolCall` → `[]byte jsonArgs` | Low | Marshal `ToolCall.Input` to JSON |
| `ToolResponse` → `any` | Medium | Extract text/image/media content; handle `StopTurn`, `IsError` |
| Permission gate → BeforeToolCallback | Medium | Wrap permission service as a `BeforeToolCallbackStructured` |
| Context keys → trpc context | Low | Inject session/model keys into stdlib context; add `ToolCallID` |
| Job tools → Streaming | High | `bash`/`job_output`/`job_input`/`job_kill` → `StreamableTool` with `StreamReader` |
| LSP integration | N/A | trpc has no LSP — keep mocode LSP tools as-is, wrapped via adapter |

### 4.2 Tools Requiring No Adaptation (Pure Wrapping)

These tools can be trivially wrapped — only the interface shim is needed:
- `view` ↔ `read_file`
- `ls` ↔ `list_file` (with reduced functionality)
- `grep` ↔ `search_content`
- `glob` ↔ `search_file`
- `transfer_to_agent` ↔ `transfer_to_agent`
- `write` ↔ `save_file`
- `edit` ↔ `replace_content`
- `fetch` ↔ webfetch

### 4.3 Tools Requiring Custom Adapter Logic

| mocode Tool | Challenge |
|---|---|
| `bash` + job tools | Must bridge mocode's job model to trpc's streaming model, or keep job abstraction |
| `multiedit` | No trpc batch-edit — must decompose into multiple `replace_content` calls |
| `crawl` | No trpc crawler — must keep mocode implementation |
| `todos`, session tools | Stateful, session-scoped — no trpc equivalent |
| `diagnostics`, `references` | LSP-dependent — no trpc equivalent |
| MCP tools | Different lifecycle: mocode uses per-call permission; trpc uses `mcpSessionManager` with reconnect |
| Memory tools | No trpc equivalent — must keep mocode's `memory.Service` |

---

## 5. Summary Statistics

| Metric | mocode | trpc-agent-go |
|---|---|---|
| **Total tool count** | ~45 (including MCP dynamic) | ~15 built-in + MCP dynamic |
| **Tool categories** | 14 | 0 (flat) |
| **Builtin vs plugin** | 10 builtin + 35 plugin | All built-in |
| **Streaming support** | Job-based (4 tools) | Native `StreamableTool` |
| **Permission system** | Built-in per-call | None |
| **Callback system** | Extension manager (separate) | Built-in `Callbacks` struct |
| **MCP integration** | Full (dynamic discovery + permission) | Full (reconnect + singleflight) |
| **Agent-as-tool** | No (coordinator pattern) | Yes (`agent.Tool`) |
| **Skill execution** | Skill system (not tool-exposed) | `skill_run`, `skill_load`, etc. |
| **Result merging** | N/A | `tool.Merge[T]` generic |
