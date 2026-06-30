## 1. Scaffold the toolkit package

- [x] 1.1 Create `tools/` directory at module root with `doc.go` describing the toolkit's purpose and import policy
- [x] 1.2 Add `tools/contracts.go` defining `Tool`, `ToolProvider`, `ToolContext`, `ToolResult`, `Schema`, `Capability`, `PermissionChecker`, `MCPHandles`, `ToolCallbacks`, and `Attachment` types per the `tool-contracts` spec
- [x] 1.3 Add `tools/registry.go` with the default `Registry` singleton, `Register`, `RegisterProvider`, `Get`, `Names`, `All`, and a constructor `NewRegistry`
- [x] 1.4 Add `tools/loader.go` (or extend registry) implementing the init-time `Register` pattern with duplicate-name warning via `slog.Warn`
- [x] 1.5 Add `tools/filter/filter.go` (moved from `internal/core/agent/tools/filter/filter.go`) wired to the new `Capability` and registry types
- [x] 1.6 Add `tools/registry_test.go` covering init-time registration, duplicate-name behavior, concurrent `Register`/`Get`, and `NewRegistry` isolation
- [x] 1.7 Verify `go build ./tools/...` and `go test ./tools/...` pass with no other consumers yet

## 2. Build the agent-side adapter

- [x] 2.1 Add `agentToolContext` struct in `internal/core/agent/` that implements `tools.ToolContext` by wrapping the existing helpers (`*toolutil.Callbacks`, permission checker, MCP handles, working dir, session id)
- [x] 2.2 Add a `NewToolContext(...)` constructor on the agent package that builds the adapter from the existing agent wiring
- [x] 2.3 Add a test that constructs a fake `tools.ToolContext` and verifies the agent adapter exposes the expected values (session id, working dir, permissions, callbacks, MCP handles)
- [x] 2.4 Add optional runtime dependency carrier support so migrated tools can resolve agent-owned services through `ToolContext` without importing agent/config packages

## 3. Move built-in tools

- [ ] 3.1 Create `tools/builtin/bash/` and move `bash.go`, `safe.go`, `bash.tpl`, and tests; add `init()` calling `tools.Register`
- [ ] 3.2 Create `tools/builtin/edit/` and move `edit.go` and `edit.md`; add `init()`
- [ ] 3.3 Create `tools/builtin/view/` and move `view.go` and `view.md`; add `init()`
- [ ] 3.4 Create `tools/builtin/ls/` and move `ls.go` and `ls.md`; add `init()`
- [ ] 3.5 Create `tools/builtin/write/` and move `write.go` and `write.md`; add `init()`
- [ ] 3.6 Create `tools/builtin/multiedit/` and move `multiedit.go` and `multiedit.md`; add `init()`
- [ ] 3.7 Create `tools/builtin/read_files/` and move `read_files.go` and `read_files.md`; add `init()`
- [x] 3.8 Create `tools/builtin/job_input/`, `tools/builtin/job_kill/`, `tools/builtin/job_output/` and move their files; add `init()` to each
- [ ] 3.9 Create `tools/builtin/all/all.go` that blank-imports every builtin subpackage; verify `go build ./tools/builtin/all/...` registers all 10
- [ ] 3.10 Update the legacy `internal/core/agent/tools/builtin/<name>/<name>.go` to re-export from the new path so the agent still builds during the cutover window
- [ ] 3.11 Run `go test ./tools/builtin/...` and confirm every builtin test passes

## 4. Move plugin tools

- [x] 4.1 Move `tools/plugins/think/` (single tool); add `init()`
- [x] 4.2 Move `tools/plugins/todos/`; add `init()`
- [x] 4.3 Move `tools/plugins/glob/` and `tools/plugins/grep/` (search common); add `init()`
- [x] 4.4 Move `tools/plugins/web_search/` and `tools/plugins/web_fetch/`; add `init()`
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
