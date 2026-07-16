# Task Plan: Toolkit Migration Handoff

## Goal
Move the agent tool surface from `internal/core/agent/tools/...` to the top-level `tools/...` toolkit, then cut the agent runtime over to the new registry without behavior drift.

## Current Phase
Phase 2: migrate remaining tool implementations.

## Current Status Summary
The migration is partially complete. The scaffold and agent-side `ToolContext` adapter exist, and a small subset of tools has been copied/moved to the new top-level toolkit. The runtime still uses the legacy registry in `internal/core/agent/tools.NewRegistry().Build(...)`, so the new toolkit is not yet the active agent tool source.

Important: existing tools contain subtle behavior changes and local customizations. Migration must be parity-driven, not a mechanical copy. See `.plans/toolkit-migration-spec.md` and `.plans/toolkit-migration-test-matrix.md` before implementing any tool move.

## Phases

### Phase 0: Parity Design and Test Gates
- [x] Write migration spec with behavior-preservation constraints.
- [x] Write tool-by-tool test matrix.
- [ ] Add adapter tests for schema/error/metadata conversion.
- [ ] Add dependency-boundary test for `tools/...` forbidden imports.
- [ ] Add parity tests for already migrated tools before runtime cutover.
- **Status:** in_progress.

### Phase 1: Confirm Scaffold and Contracts
- [x] Confirm `tools/` scaffold exists: `contracts.go`, `registry.go`, `loader.go`, `runtime.go`, `result.go`.
- [x] Confirm `tools/filter` exists.
- [x] Confirm agent-side `agentToolContext` exists in `internal/core/agent/agent_tool_context.go`.
- [x] Confirm migration spec and tasks exist under `openspec/changes/agent-architecture-refactor/`.
- **Status:** completed.

### Phase 2: Migrate Remaining Builtins
- [ ] Move or adapt `bash`, including `safe.go`, templates, tests, permission behavior, background job metadata.
- [ ] Move or adapt `edit`, preserving read-before-edit/file tracker/LSP behavior.
- [ ] Move or adapt `view`, preserving skill-resource handling and file tracker behavior.
- [ ] Move or adapt `ls`, preserving config-driven options.
- [ ] Move or adapt `write`, preserving permission, file tracker, and LSP behavior.
- [ ] Move or adapt `multiedit`, preserving exact-match semantics and failed edit reporting.
- [ ] Move or adapt `read_files`, preserving glob expansion and file tracker behavior.
- [ ] Update `tools/builtin/all/all.go` to include all builtins.
- [ ] Add or update registration tests asserting all 10 builtin names are present.
- [ ] Run `go test ./tools/builtin/...` and targeted legacy tests after each moved group.
- **Status:** in_progress.

### Phase 3: Migrate Remaining Plugins
- [ ] Move network plugins: `crawl`, `fetch`, `download`, `download_docs`.
- [ ] Move SSH plugins and shared `sshcommon`.
- [ ] Move git operation plugins and shared `gitopscommon`.
- [ ] Move Gitea plugins and shared `giteacommon`.
- [ ] Move `sourcegraph`.
- [ ] Move WeChat and export plugins: `send_wechat_file`, `send_wechat_image`, `screenshot_to_wechat`, `message_export`.
- [ ] Move session plugins: `session_export`, `session_search`, `session_summary`.
- [ ] Move mocode diagnostics plugins: `mocode_info`, `mocode_logs`.
- [ ] Move `lsp_restart`.
- [ ] Move MCP resource plugins: `list_mcp_resources`, `read_mcp_resource`.
- [ ] Update `tools/plugins/all/all.go` to include every plugin.
- [ ] Add or update registration tests asserting every plugin name is present.
- [ ] Run `go test ./tools/plugins/...` and targeted legacy tests after each moved group.
- **Status:** pending.

### Phase 4: Migrate MCP and LSP Bridges
- [ ] Move `internal/core/agent/tools/mcp` to `tools/mcp` with a live-state `ToolProvider` shape.
- [ ] Move `internal/core/agent/tools/lsp` to `tools/lsp`, resolving clients lazily via `ToolContext` or runtime dependencies.
- [ ] Keep lifecycle ownership in app/agent wiring; do not make toolkit own long-lived app state.
- [ ] Run `go test ./tools/mcp/... ./tools/lsp/...`.
- **Status:** pending.

### Phase 5: Cut Over Runtime
- [ ] Add the missing adapter from `tools.Tool` to `fantasy.AgentTool` if not already present.
- [ ] Change `Coordinator` from `internal/core/agent/tools.NewRegistry().Build(ctx, deps)` to the top-level toolkit registry plus adapter.
- [ ] Blank-import `tools/builtin/all` and `tools/plugins/all` from the agent runtime opt-in location.
- [ ] Keep agent-level workflow tools (`agentic_fetch`, `roundtable`, likely `hooked_tool`) in agent unless design changes.
- [ ] Remove direct per-tool imports from `internal/core/agent`.
- [ ] Verify no `internal/core/agent/tools` import remains in `internal/core/agent` after cutover.
- **Status:** pending.

### Phase 6: Delete Legacy Tool Tree
- [ ] Convert any temporary legacy re-export files if needed during cutover.
- [ ] Delete `internal/core/agent/tools/` only after runtime, UI, transport, and tests no longer import it.
- [ ] Update UI/chat/dialog imports to stable replacement packages or domain-level constants where needed.
- [ ] Run `go build ./...` and targeted tests.
- **Status:** pending.

### Phase 7: Docs, Governance, and Full Verification
- [ ] Update `AGENTS.md` with new tool layout and one-line opt-in rule.
- [ ] Update architecture docs or add a toolkit contract doc.
- [ ] Update `CONTRIBUTING.md` with how to add a tool.
- [ ] Add dependency-boundary check for `tools/...` not importing forbidden packages.
- [ ] Run `go test ./tools/...`, `go test ./internal/core/agent/...`, `go build ./...`, then broader tests as resources allow.
- **Status:** pending.

## Key Questions
1. Should legacy paths re-export from new paths during the cutover, or should the branch perform a direct flag-day switch? The existing task list says re-export, while the design says avoid dual registration and keep PRs green.
2. Where should UI-facing tool constants live after deleting `internal/core/agent/tools`? UI currently imports old tool constants for rendering.
3. Does the new `tools.Schema` have enough fidelity for all current `fantasy.ToolSchema` usage, including required fields and JSON schema edge cases?
4. Should `sourcegraph` remain a direct public web-code-search plugin, or be grouped under search/network in the new capability taxonomy?

## Decision Log
| Decision | Rationale | Date |
|----------|-----------|------|
| Keep runtime on legacy registry until all migrated tools and adapter parity are ready. | Avoid partial cutover and duplicate active registrations. | 2026-07-17 |
| Treat `tools/` as the future source of truth, not a second active tool system. | Matches OpenSpec design and prevents behavior drift. | 2026-07-17 |
| Preserve agent-level workflow tools outside top-level toolkit initially. | Design explicitly assumes `agentic_fetch` and `roundtable` are not generic toolkit tools. | 2026-07-17 |
