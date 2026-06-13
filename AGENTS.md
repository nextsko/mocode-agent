# Mocode Development Guide

## Project Overview

Mocode is a terminal-based AI coding assistant built in Go by
[Charm](https://charm.land). It connects to LLMs and gives them tools to read,
write, and execute code. It supports multiple providers (Anthropic, OpenAI,
Gemini, Bedrock, MiniMax, Vercel, and more), integrates with
LSPs for code intelligence, and supports extensibility via MCP servers and
agent skills.

The module path is `github.com/package-register/mocode`.

## Architecture

```
main.go                            CLI entry point (cobra via internal/cmd)
internal/
  app/app.go                       Top-level wiring: DB, config, agents, LSP, MCP, events
  cmd/                             CLI commands (root, run, login, models, stats, sessions)
  config/
    config.go                      Config struct, context file paths, agent definitions
    load.go                        Mocode.json loading and validation
    provider.go                    Provider configuration and model resolution
  agent/
    agent.go                       SessionAgent: runs LLM conversations per session
    coordinator.go                 Coordinator: manages named agents ("coder", "task")
    roundtable/                    Multi-agent roundtable team mode engine
    roundtable_tool.go             Coordinator-owned roundtable tool
    hooked_tool.go                 Decorator that runs PreToolUse hooks before tool execution
    prompts.go                     Loads Go-template system prompts
    templates/                     System prompt templates (coder.md.tpl, task.md.tpl, etc.)
    tools/                         Tool system façade + central registry
      registry.go                  Registry type, ToolPlugin interface, all standard plugins
      compat.go                    Backward-compat re-exports (constants, types, funcs)
      builtin/exec/                Core exec tools (bash, job_output, job_kill)
      builtin/file/                Core file tools (edit, multiedit, view, write, ls)
      plugins/                     Optional/role-specific tool plugins
        search/                    glob, grep, sourcegraph
        network/                   fetch, crawl, download, download_docs
        lsp/                       lsp_diagnostics, lsp_references, lsp_restart
        mocode/                    mocode_info, mocode_logs
        session/                   todos, session_export, session_summary
        mcp/                       list_mcp_resources, read_mcp_resource
        memory/                    delegates to memory.Service.Tools()
        think/                     think
        gitea/                     gitea_issues, gitea_pulls, gitea_notifications
      internal/                    Shared helpers (retry, context keys, toolctx, etc.)
      mcp/                         MCP client: connection pool, tool execution
  hooks/                           Hook engine: runs user shell commands on hook events
    hooks.go                       Decision types, aggregation logic, event constants
    runner.go                      Parallel hook execution, timeout, dedup
    input.go                       Stdin payload builder, env vars, stdout parsing (Mocode + Claude Code compat)
  session/session.go               Session CRUD backed by SQLite
  message/                         Message model and content types
  db/                              SQLite via sqlc, with migrations
    sql/                           Raw SQL queries (consumed by sqlc)
    migrations/                    Schema migrations
  lsp/                             LSP client manager, auto-discovery, on-demand startup
  ui/                              Bubble Tea v2 TUI (see internal/ui/AGENTS.md)
  permission/                      Tool permission checking and allow-lists
  skills/                          Skill file discovery and loading
  shell/                           Bash command execution with background job support
  metadata/                        Machine fingerprint and system metadata (privacy-safe)
  pubsub/                          Internal pub/sub for cross-component messaging
  filetracker/                     Tracks files touched per session
  history/                         Prompt history
```

### Key Dependency Roles

- **`charm.land/fantasy`**: LLM provider abstraction layer. Handles protocol
  differences between Anthropic, OpenAI, Gemini, etc. Used in `internal/app`
  and `internal/agent`.
- **`charm.land/bubbletea/v2`**: TUI framework powering the interactive UI.
- **`charm.land/lipgloss/v2`**: Terminal styling.
- **`charm.land/glamour/v2`**: Markdown rendering in the terminal.
- **`charm.land/catwalk`**: Snapshot/golden-file testing for TUI components.
- **`sqlc`**: Generates Go code from SQL queries in `internal/db/sql/`.

### Key Patterns

- **Config is a Service**: accessed via `config.Service`, not global state.
- **Tools are self-documenting**: each tool has a `.go` implementation and a
  `.md` description file in `internal/agent/tools/`.
- **System prompts are Go templates**: `internal/agent/templates/*.md.tpl`
  with runtime data injected.
- **Context files**: Mocode reads AGENTS.md, Mocode.md, CLAUDE.md, GEMINI.md
  (and `.local` variants) from the working directory for project-specific
  instructions.
- **Persistence**: SQLite + sqlc. All queries live in `internal/db/sql/`,
  generated code in `internal/db/`. Migrations in `internal/db/migrations/`.
- **Pub/sub**: `internal/pubsub` for decoupled communication between agent,
  UI, and services.
- **Hooks**: User-defined shell commands in `Mocode.json` that fire before
  tool execution. The engine (`internal/hooks/`) is independent of fantasy
  and agent 鈥?it takes inputs, runs commands, returns decisions. The
  `hookedTool` decorator in `internal/agent/hooked_tool.go` wraps tools at
  the coordinator level. Hooks run before permission checks. See
  `HOOKS.md` for the user-facing protocol.
- **CGO disabled**: builds with `CGO_ENABLED=0` and
  `GOEXPERIMENT=greenteagc`.

## Build/Test/Lint Commands

- **Version bump**: Use `task version:show` to inspect the current internal version and next semver tag, use `task version:bump` to automatically bump `internal/version/version.go`, use `task version:set VERSION=x.y.z` to set a specific version manually, and use `task release` to bump the version, create the release commit, tag it, and push it in one flow.

- **Build**: `go build -buildvcs=false .` or `go run -buildvcs=false .`
- **Test**: `task test` or `go test ./...` (run single test:
  `go test ./internal/llm/prompt -run TestGetContextFromPaths`)
- **Update Golden Files**: `go test ./... -update` (regenerates `.golden`
  files when test output changes)
  - Update specific package:
    `go test ./internal/tui/components/core -update` (in this case,
    we're updating "core")
- **Lint**: `task lint:fix`
- **Format**: `task fmt` (`gofumpt -w .`)
- **Modernize**: `task modernize` (runs `modernize` which makes code
  simplifications)
- **Dev**: `task dev` (runs with profiling enabled)

## Post-Change Local Pipeline

After every meaningful code change, run this local pipeline before committing.
The goal is to catch build, test, format, and lint issues on the agent side,
so the repository stays green for the next contributor.

1. **Build the binary.**
   ```bash
   go build -buildvcs=false .
   ```
   If this fails, fix compilation errors before moving on.

2. **Run tests for the packages you touched.**
   ```bash
   go test ./path/to/package/...
   ```
   Prefer targeted tests first; run `go test ./...` when the change is broad
   or touches shared packages. On Windows, some `internal/fsext` tests may
   fail due to path-separator assumptions; those are pre-existing and should
   not block unrelated work.

3. **Format the code.**
   ```bash
   task fmt
   # or, if gofumpt is not available:
   gofmt -w .
   ```
   Always format before linting so format-only diffs do not obscure real
   issues in review.

4. **Run the linter and fix what you can.**
   ```bash
   task lint:fix
   ```
   If `lint:fix` cannot auto-fix a violation, resolve it manually or add a
   documented `//nolint` exception with a reason.

5. **Check the diff.**
   ```bash
   git status --short
   git diff --stat
   ```
   Make sure only intended files are modified and no debug code, secrets, or
   generated artifacts are included.

6. **Update AGENTS.md or other docs if the change affects conventions.**
   If you modified architecture, build steps, tool behavior, or coding style,
   keep the corresponding docs in sync.

7. **Commit with a semantic message.**
   ```bash
   git commit -m "feat: short description of the change"
   ```
   Use `fix:`, `feat:`, `refactor:`, `docs:`, `test:`, or `chore:` as
   appropriate. Keep the summary line under 72 characters.

8. **Run a final targeted verification after committing (optional but
   recommended).**
   ```bash
   go test ./...
   ```
   This confirms the committed state is still green.

## Code Style Guidelines

- **Imports**: Use `goimports` formatting, group stdlib, external, internal
  packages.
- **Formatting**: Use gofumpt (stricter than gofmt), enabled in
  golangci-lint.
- **Naming**: Standard Go conventions 鈥?PascalCase for exported, camelCase
  for unexported.
- **Types**: Prefer explicit types, use type aliases for clarity (e.g.,
  `type AgentName string`).
- **Error handling**: Return errors explicitly, use `fmt.Errorf` for
  wrapping.
- **Context**: Always pass `context.Context` as first parameter for
  operations.
- **Interfaces**: Define interfaces in consuming packages, keep them small
  and focused.
- **Structs**: Use struct embedding for composition, group related fields.
- **Constants**: Use typed constants with iota for enums, group in const
  blocks.
- **Testing**: Use testify's `require` package, parallel tests with
  `t.Parallel()`, `t.SetEnv()` to set environment variables. Always use
  `t.Tempdir()` when in need of a temporary directory. This directory does
  not need to be removed.
- **JSON tags**: Use snake_case for JSON field names.
- **File permissions**: Use octal notation (0o755, 0o644) for file
  permissions.
- **Log messages**: Log messages must start with a capital letter (e.g.,
  "Failed to save session" not "failed to save session").
  - This is enforced by `task lint:log` which runs as part of `task lint`.
- **Comments**: End comments in periods unless comments are at the end of the
  line.

## Testing with Mock Providers

When writing tests that involve provider configurations, use the mock
providers to avoid API calls:

```go
func TestYourFunction(t *testing.T) {
    // Enable mock providers for testing
    originalUseMock := config.UseMockProviders
    config.UseMockProviders = true
    defer func() {
        config.UseMockProviders = originalUseMock
        config.ResetProviders()
    }()

    // Reset providers to ensure fresh mock data
    config.ResetProviders()

    // Your test code here - providers will now return mock data
    providers := config.Providers()
    // ... test logic
}
```

## Formatting

- ALWAYS format any Go code you write.
  - First, try `gofumpt -w .`.
  - If `gofumpt` is not available, use `goimports`.
  - If `goimports` is not available, use `gofmt`.
  - You can also use `task fmt` to run `gofumpt -w .` on the entire project,
    as long as `gofumpt` is on the `PATH`.

## Comments

- Comments that live on their own lines should start with capital letters and
  end with periods. Wrap comments at 78 columns.

## Committing

- ALWAYS use semantic commits (`fix:`, `feat:`, `chore:`, `refactor:`,
  `docs:`, `sec:`, etc).
- Try to keep commits to one line, not including your attribution. Only use
  multi-line commits when additional context is truly necessary.

## Working on the TUI (UI)

Anytime you need to work on the TUI, read `internal/ui/AGENTS.md` before
starting work.

## Known Code Hotspots

The following files are unusually large and centralize many responsibilities.
Treat them as "god files": avoid adding new concerns to them; prefer extracting
new packages or files instead.

- **`internal/ui/model/ui.go`** — the most prominent god file in the project.
  It is ~3,240 lines, contains ~75 methods/functions, 21 type definitions, and
  54 imports. It centralizes the top-level Bubble Tea model: state machine,
  message routing, layout, session lifecycle, agent runtime tracking, MCP
  management, clipboard/paste handling, and WeChat integration. Any new TUI
  feature should be implemented in a dedicated sub-package or model file, not
  by expanding this file.

- **`internal/agent/coordinator.go`** (~1,430 lines, 41 methods) — coordinates
  named agents and their lifecycle.

- **`internal/app/app.go`** (~1,020 lines, 74 methods) — wires together nearly
  every service (sessions, messages, history, permissions, LSP, memory,
  pub/sub, agent coordinator, error collector).

- **`internal/ui/model/chat.go`** (~980 lines, 51 methods) — chat view,
  scrolling, animations, and mouse handling.

- **`internal/ui/model/ui_dialogs.go`** (~980 lines, 28 methods) — central
  dialog dispatch and handling.

- **`internal/ui/dialog/permissions.go`** (~790 lines, 34 methods) — permission
  dialog logic.

`internal/swagger/docs.go` is larger still, but it is auto-generated Swagger
documentation and not a concern for manual refactoring.

## Environment Quirks

### Bash tool stdout capture

On this Windows host the `Bash` tool originally executed successfully but did
**not** return stdout to the conversation. The root cause was the
`KIMI_SHELL_PATH` environment variable: it pointed to `git-bash.exe`, but the
Kimi Code CLI documentation says it must point to **`bash.exe`**.
`git-bash.exe` is the Git for Windows launcher and is not meant for
non-interactive `-c` script execution; `bin/bash.exe` is the correct target.

**Status: fixed.** `KIMI_SHELL_PATH` is now set to
`C:\Program Files\software\envs\Git\bin\bash.exe` and the `Bash` tool returns
stdout normally.

If the variable regresses, the correct value is:

```
KIMI_SHELL_PATH=C:\Program Files\software\envs\Git\bin\bash.exe
```

## Troubleshooting Methodology

When a tool behaves unexpectedly, follow this playbook before guessing at code
changes.

### 1. Separate "the tool failed" from "the command failed"

- Did the command report an exit code? If yes, the command itself failed.
- Did the command report success but produce no visible output? The tool's
  output channel may be broken or misrouted.
- Did the command hang or time out? It may be waiting for input, a TTY, or a
  network resource.

### 2. Reduce the command to the smallest reproducible case

Start with the simplest possible command and escalate only when it works:

```bash
echo hello
some-command | head -1
some-command > tmp/output.txt 2>&1 && cat tmp/output.txt
```

If even `echo hello` does not return output, the shell or output capture layer
is broken, not the command.

### 3. Check environment variables that control the tool

Many CLI agents read shell-related variables before launching commands. On
Windows with Git Bash, check at least:

- `KIMI_SHELL_PATH`
- `HERMES_GIT_BASH_PATH`
- `SHELL`
- `PATH`

Compare the actual values against the official documentation. A path ending in
`git-bash.exe` instead of `bash.exe` is a common misconfiguration.

### 4. Test the shell binary directly

Verify that the configured shell binary works for non-interactive execution:

```bash
"/c/Program Files/software/envs/Git/bin/bash.exe" -c "echo test"
"/c/Program Files/software/envs/Git/git-bash.exe" -c "echo test"
```

If one works and the other does not, the shell path is the problem.

### 5. Read vendor documentation before fixing

Check the vendor docs for the exact required value. Do not assume the current
value is correct just because it exists. Record the canonical source in the
project notes.

### 6. Verify the fix end-to-end

After changing the environment, test the original failing command plus a few
variations:

- plain stdout
- piped output
- heredocs
- external command calls (`go version`, `python3 -c ...`)

Only mark the issue resolved when all variations work without workarounds.

### 7. Record the resolution where future agents will see it

Update `AGENTS.md` with:

- the symptom
- the root cause
- the canonical fix
- the verification steps
- any remaining workarounds

This prevents the same debugging loop from happening in the next session.

## Provider Behavior Observations

We analyzed 47,298 messages from local session logs
(`C:\Users\16143\AppData\Local\mocode\projects`) covering 6,628 `bash` tool
calls. Several provider-specific command patterns emerged.

### Command preferences

| Provider      | `head`     | `grep`     | `rg`       | Notes                          |
|---------------|-----------:|-----------:|-----------:|--------------------------------|
| minimax-china | 306 (6.8%) | 170 (3.8%) | 5 (0.1%)   | Strongly prefers `head`/`grep` |
| opencode-go   | 55 (5.0%)  | 36 (3.3%)  | 36 (3.3%)  | Uses `rg` as often as `grep`   |
| xiaomi-coding | 43 (5.5%)  | 31 (4.0%)  | 5 (0.6%)   | Similar to MiniMax             |
| kimi-coding   | 8 (3.1%)   | 5 (2.0%)   | 1 (0.4%)   | Small sample                   |

- **MiniMax** favors `head` for truncating command output on Windows and almost
  never uses `rg`. Typical call:
  ```bash
  python "C:\Users\...\tmp\check.py" 2>&1 | head -80
  ```
- **opencode-go** is comfortable with `rg` and uses it at roughly the same rate
  as `grep`.

### Read-before-write discipline

A naive "same-session edit/write without prior read/view" check returned 0%,
but evolution logs and error patterns show a related problem: **MiniMax often
fails to re-read a file before editing it** or applies an edit based on a stale
snapshot. Common failure signatures:

- `old_string not found` from `edit`/`multiedit` (MiniMax)
- `file has been modified since it was last read` before `write`/`edit`
  (MiniMax)

This suggests the issue is not "never read the file" but "trust an outdated or
partial view when editing."

### Implications for prompts and tools

When working with providers in this project, consider biasing the system prompt
or tool descriptions:

- **MiniMax**: explicitly require a fresh `view`/`read_file` before every
  `edit`/`write`; discourage `head`/`tail` for inspection because they hide
  context needed for edits; prefer `rg`/`grep` only for locating, then `view`
  for reading.
- **opencode-go**: `rg` usage is healthy; keep encouraging it.
- **All providers**: after any external process modifies a file (build,
  formatter, another tool), re-read before editing.

## Active Refactor Plan

A comprehensive TUI refactor plan is in progress. Before making any non-trivial
changes to `internal/ui/`, read the plan at:

```
.mocode/plans/tui-refactor/
```

Start with `00-readme.md`, then `01-requirements.md`, `02-architecture.md`, and
`03-roadmap.md`. Implementation tasks are in `tasks/` and are designed to be
picked up by parallel agents. Domain analysis reports are in `reviews/`.
