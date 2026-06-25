# Hooks Protocol

User-defined shell commands in `Mocode.json` that fire on hook events (e.g.
`PreToolUse`). The engine lives in [`internal/hooks/`](internal/hooks/).

## Configuration

Hooks are defined in workspace or global config under the `hooks` key. Each
hook specifies:

- **event** — e.g. `PreToolUse`
- **matcher** — optional tool name pattern
- **command** — shell command to run

## Decisions

Hooks return one of:

| Decision | Meaning |
|----------|---------|
| allow | Explicitly allow the action |
| deny | Block the action |
| none | No opinion (pass through) |

Exit code **49** halts the entire agent turn.

## Integration

- Hooks run **before** permission checks via `hookedTool` in
  [`internal/agent/hooked_tool.go`](internal/agent/hooked_tool.go).
- Hook metadata is embedded in tool responses for UI display.

## See Also

- [`internal/hooks/hooks.go`](internal/hooks/hooks.go) — decision types and aggregation
- [`internal/hooks/runner.go`](internal/hooks/runner.go) — parallel execution
- [`internal/hooks/input.go`](internal/hooks/input.go) — stdin payload and env vars
