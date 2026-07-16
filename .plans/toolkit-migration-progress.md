# Toolkit Migration Progress

## Session: 2026-07-17

### Phase 1: Inventory and Handoff
- **Status:** completed
- Operations performed:
  - Located official migration spec under `openspec/changes/agent-architecture-refactor/`.
  - Compared old legacy tool tree and new top-level toolkit tree.
  - Confirmed runtime still consumes legacy `internal/core/agent/tools` registry.
  - Created persistent handoff plan files under `.plans/`.
- Files created/modified:
  - `.plans/toolkit-migration-task_plan.md`
  - `.plans/toolkit-migration-progress.md`
  - `.plans/toolkit-migration-findings.md`

## Migration Coverage Snapshot

### Completed by existing work
- Toolkit scaffold: `tools/doc.go`, `contracts.go`, `registry.go`, `loader.go`, `runtime.go`, `result.go`.
- Toolkit filter: `tools/filter`.
- Agent context adapter: `internal/core/agent/agent_tool_context.go`.
- Builtins migrated to top-level toolkit:
  - `job_input`
  - `job_kill`
  - `job_output`
- Plugins migrated to top-level toolkit:
  - `glob`
  - `grep`
  - `think`
  - `todos`
  - `web_fetch`
  - `web_search`
- Shared helper packages migrated for already moved plugins:
  - `tools/plugins/searchcommon`
  - `tools/plugins/netcommon`

### Still legacy-only or not cut over
- Main agent runtime still calls `internal/core/agent/tools.NewRegistry().Build(ctx, deps)` in `internal/core/agent/coordinator.go`.
- Most builtins remain under `internal/core/agent/tools/builtin`.
- Most plugins remain under `internal/core/agent/tools/plugins`.
- MCP and LSP bridges remain under `internal/core/agent/tools/mcp` and `internal/core/agent/tools/lsp`.
- UI, transport, app, and chat rendering still import legacy paths for constants and types.

## Test Results
| Test | Input | Expected | Actual | Status |
|------|-------|----------|--------|--------|
| Inventory only | N/A | No code behavior change | No behavior tests run in first inventory pass | Not run |
| Toolkit baseline | `go test ./tools/...` | Existing top-level toolkit tests pass | Passed | Pass |
| Legacy tools baseline | `go test ./internal/core/agent/tools/...` | Current active legacy tool tests pass | Passed | Pass |
| Agent baseline | `go test ./internal/core/agent/...` | Agent package and tool tests pass | Passed | Pass |

## Design Update: 2026-07-17
- Added `.plans/toolkit-migration-spec.md`.
- Added `.plans/toolkit-migration-test-matrix.md`.
- Updated task plan with Phase 0 parity gates.
- Recorded that migrated tools may contain behavior drift and must be parity-tested before runtime cutover.

## Error Log
| Time | Error | Attempts | Solution |
|------|-------|----------|----------|
| 2026-07-17 | None during inventory | N/A | N/A |

## 5-Question Reboot Check
| Question | Answer |
|----------|--------|
| Where am I? | Starting Phase 2: migrate remaining tool implementations. |
| Where am I going? | Finish top-level toolkit migration, cut runtime over, delete legacy tool tree. |
| What is the goal? | Make `tools/...` the single active tool surface and remove `internal/core/agent/tools/...`. |
| What have I learned? | Scaffold and some tools are migrated; runtime is still legacy; the next safe work is continuing implementation migration before cutover. |
| What have I done? | Created persistent plan/progress/findings files and captured migration coverage. |
