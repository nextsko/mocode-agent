# Toolkit Migration Findings and Decisions

## Requirements
- Move every LLM-callable tool from `internal/core/agent/tools/...` into top-level `tools/...`.
- Preserve tool names, schemas, behavior, tests, and side effects.
- Make adding a tool a one-line blank import through `tools/builtin/all` or `tools/plugins/all`.
- Keep `tools/...` framework-agnostic: it must not import `charm.land/fantasy`, `charm.land/catwalk`, `internal/core/agent`, or `internal/core/config`.
- Cut the agent runtime over to the new toolkit registry and delete the legacy tree when no imports remain.

## Source Files
| Path | Role |
|------|------|
| `openspec/changes/agent-architecture-refactor/tasks.md` | Official migration checklist. |
| `openspec/changes/agent-architecture-refactor/design.md` | Migration strategy and architectural decisions. |
| `openspec/changes/agent-architecture-refactor/specs/agent-toolkit/spec.md` | Behavioral requirements for toolkit migration. |
| `tools/` | New top-level toolkit scaffold and partial implementations. |
| `internal/core/agent/tools/` | Legacy active tool tree. |
| `internal/core/agent/coordinator.go` | Runtime registry integration still using legacy registry. |
| `internal/core/agent/agent_tool_context.go` | New agent-side `ToolContext` adapter. |

## Current Tool Mapping
| Tool/group | New toolkit status | Legacy status | Notes |
|------------|--------------------|---------------|-------|
| `job_input`, `job_kill`, `job_output` | Present under `tools/builtin/*` | Still present under legacy path | Migrated but not runtime-active. |
| `glob`, `grep` | Present under `tools/plugins/*` | Still present under legacy path | Migrated with `searchcommon`. |
| `think`, `todos` | Present under `tools/plugins/*` | Still present under legacy path | Migrated but runtime still uses old implementation. |
| `web_fetch`, `web_search` | Present under `tools/plugins/*` | Still present under legacy path | Migrated with `netcommon`; not exposed through legacy registry's network group except old web tools if configured elsewhere. |
| `bash`, `edit`, `view`, `ls`, `write`, `multiedit`, `read_files` | Missing under new toolkit | Present under legacy path | Highest-priority remaining builtins. |
| `crawl`, `fetch`, `download`, `download_docs` | Missing under new toolkit | Present under legacy path | Network group next after builtins. |
| SSH group | Missing under new toolkit | Present under legacy path | Requires moving `sshcommon`. |
| Git ops group | Missing under new toolkit | Present under legacy path | Requires moving `gitopscommon`. |
| Gitea group | Missing under new toolkit | Present under legacy path | Requires moving `giteacommon`. |
| `sourcegraph` | Missing under new toolkit | Present under legacy path | Search plugin, likely migrate near `glob/grep`. |
| Session/export/mocode/wechat/LSP/MCP plugins | Missing under new toolkit | Present under legacy path | Need runtime dependencies or bridge design. |
| MCP bridge | Missing under new toolkit | Present under legacy path | Must become live-state `ToolProvider`. |
| LSP bridge | Missing under new toolkit | Present under legacy path | Must lazily resolve client via `ToolContext` or runtime dependencies. |

## Key Findings
- There are currently two tool systems in the repository, but only the legacy system is active in the agent runtime.
- `tools/` is intentionally the migration target, not accidental duplication.
- The migration task list says Phase 1 and Phase 2 are complete, Phase 3+ are mostly pending.
- Actual files confirm the task list: only three builtins and six plugins are present under the new toolkit.
- `tools/builtin/all/all.go` currently imports only job tools, so it does not satisfy the final requirement of all 10 builtins.
- `tools/plugins/all/all.go` currently imports only `glob`, `grep`, `think`, `todos`, `web_fetch`, and `web_search`, so it does not satisfy the final requirement of all plugins.
- UI and transport packages import legacy tool constants/types directly. Deleting legacy paths will require either moving constants to top-level toolkit or adding UI-safe domain constants.
- The top-level toolkit has `RuntimeDependency` support, which should be used for migrated tools that need app-owned services without importing internal agent/config packages.
- Already migrated tools are not guaranteed to be exact legacy clones. `grep` lost legacy regex cache/config timeout behavior in the toolkit version; `todos` shifted some validation failures from returned Go errors to `ToolResult.Error`; job tools moved to manual schemas and toolkit metadata maps. Runtime cutover must wait for adapter/parity tests.

## Technical Decisions
| Decision | Rationale |
|----------|-----------|
| Continue implementation migration before runtime cutover. | Cutting over now would drop many tools because the new registry is incomplete. |
| Keep `tools/...` free of `fantasy` and internal agent/config imports. | Required by spec and keeps future kernel swap localized. |
| Migrate in dependency order: shared helpers, leaf tools, umbrella imports, tests. | Reduces compile breakage and behavior drift. |
| Treat old and new duplicate tools as a temporary migration state. | Runtime still uses old registry; duplicate packages are not both active unless intentionally imported. |

## Issues Encountered
| Issue | Solution |
|-------|----------|
| Two registries can confuse status assessment. | Use runtime entry point as source of truth: `coordinator.go` still uses legacy registry. |
| Existing task docs may be stale relative to files. | Cross-check task checkboxes with actual `glob` and imports before executing each phase. |
| Some migrated tools may require app services not allowed in `tools/...`. | Use `tools.RuntimeDependency` through `ToolContext`, not forbidden imports. |

## Reference Resources
- `openspec/changes/agent-architecture-refactor/tasks.md`
- `openspec/changes/agent-architecture-refactor/design.md`
- `openspec/changes/agent-architecture-refactor/specs/agent-toolkit/spec.md`
- `.plans/toolkit-migration-task_plan.md`
- `.plans/toolkit-migration-progress.md`
