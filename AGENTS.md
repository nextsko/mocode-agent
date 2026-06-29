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

main.go                            CLI entry point (cobra via internal/transport/cmd)
internal/
  core/                            Core domain logic
    agent/                         LLM agents, coordinator, tools, roundtable
      candidate/ failover/         Eval selection, model failover (agent capabilities)
      evolution/                   Self-evolution patch system (.mocode/patches)
      ctxcompress/ notify/         Context compression, notifications
      tools/                       LLM tool registry (builtin + plugins + mcp)
      toolutil/                    Shared tool helpers
    app/                           In-process composition root (store, agents, LSP, MCP)
    config/                        Mocode.json loading and providers
    crawler/                       Network fetch/scrape used by plugins
    evaluation/                    LLM-judge evaluation harness
    hooks/                         PreToolUse shell hooks (see HOOKS.md)
    knowledge/                     Memory service + kngs templates
    permission/                    Tool permission checks
    shellruntime/                  Bash/screencap execution engine
      shell/                       Shell job runner used by bash tool
      screencap/                   Screen capture
    skills/                        Agent skills discovery + builtin skills
  domain/                          Domain models
    session/                       Session + message models (message/, sessionlog/)
    types/ history/ filetracker/   Shared DTOs, prompt history, file tracking
  util/                            Standard-library-style helpers
    csync/ diff/ errcoll/ ext/     Concurrency, diff, error collection, str/path
    fsext/ infra/ log/ pubsub/     FS helpers, home/data paths, logging, event bus
    version/                       Build version
  integration/                     External integrations
    authhandler/                   OAuth login handlers
    wechat/                        WeChat bot + butler (gateway entry)
  transport/                       Entry surfaces
    cmd/                           Cobra CLI (not slash commands)
    admin/                         Local admin HTTP settings UI (127.0.0.1)
    workspace/                     Frontend facade (AppWorkspace)
  store/                           JSONL file persistence (+ sidecar indexes)
  ui/                              Bubble Tea TUI (see internal/ui/AGENTS.md)
```

### Transport layer

| Entry | Package | Role |
|-------|---------|------|
| TUI | `transport/cmd` + `ui` | Default interactive experience |
| Admin | `internal/transport/admin` | Local settings at `127.0.0.1` |
| Gateway | `internal/integration/wechat/gateway` | Persistent WeChat bot |

See [docs/architecture/control-plane.md](docs/architecture/control-plane.md).

### Key Dependency Roles

- **`charm.land/fantasy`**: LLM provider abstraction layer.
- **`charm.land/bubbletea/v2`**: TUI framework.
- **`charm.land/lipgloss/v2`**: Terminal styling.
- **`charm.land/glamour/v2`**: Markdown rendering.
- **`charm.land/catwalk`**: Golden-file testing for TUI.

### Key Patterns

- **Config is a Service**: accessed via `config.Service`, not global state.
- **Tools are self-documenting**: each tool has `.go` + `.md` in `internal/core/agent/tools/`.
- **System prompts are Go templates**: `internal/core/agent/templates/*.md.tpl`.
- **Context files**: AGENTS.md, Mocode.md, CLAUDE.md, GEMINI.md from working directory.
- **Persistence**: JSONL via `internal/store/` under `%LOCALAPPDATA%/mocode/` or `~/.local/share/mocode/`.
- **Pub/sub**: `internal/util/pubsub` for agent, UI, and services.
- **Hooks**: User shell commands in Mocode.json; engine in `internal/hooks/`. See `HOOKS.md`.
- **CGO disabled**: `CGO_ENABLED=0`, `GOEXPERIMENT=greenteagc`.

- **Layer boundaries**: verified by `go run ./scripts/layercheck` (zero upward violations). Dependency direction is strictly `transport/integration/ui -> core -> store/domain -> util`.
- **Dependency inversion via domain ports**: cross-layer needs go through interfaces in `internal/domain/` (e.g. `messenger.Messenger`, `theme.SpinnerThemer`); integration/UI provide adapters. See docs/dev-notes/structure-governance-baseline.md.
- **Agent extensions**: lifecycle hooks via `internal/core/agent/extension`; panic-recovered, duplicate-name-rejecting, AbortRun-honoring. The coordinator fires `before_run`/`after_run` events.
- **Self-evolution with quality gates**: `internal/core/agent/evolution` produces patches from session logs; candidates pass through `evolution/gates` (SpecGate -> SafetyGate) before persistence via `cmd evolve`.
- **Web search provider chain**: `netcommon.Provider` interface with fallback (DuckDuckGo HTML -> Instant Answer API); inject custom chains via `NewWebSearchToolWithProvider`.
## Build/Test/Lint Commands

- **Version bump**: Use `task version:show` to inspect the current internal version and next semver tag, use `task version:bump` to automatically bump `internal/version/version.go`, use `task version:set VERSION=x.y.z` to set a specific version manually, and use `task release` to bump the version, create the release commit, tag it, and push it in one flow.

- **Build**: `go build -buildvcs=false -o bin/mocode .` or `task build`
- **Test**: `task test` or `go test ./...` (single package:
  `go test ./internal/core/agent/prompt -run TestGetContextFromPaths`)
- **Update Golden Files**: `go test ./... -update`
  - Example: `go test ./internal/ui/diffview -update`
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
   or touches shared packages. On Windows, some `internal/util/fsext` tests may
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

- **`internal/core/agent/coordinator.go`** (~1,430 lines, 41 methods) — coordinates
  named agents and their lifecycle.

- **`internal/core/app/app.go`** (~1,020 lines, 74 methods) — wires together nearly
  every service (sessions, messages, history, permissions, LSP, memory,
  pub/sub, agent coordinator, error collector).

- **`internal/ui/model/chat.go`** (~980 lines, 51 methods) — chat view,
  scrolling, animations, and mouse handling.

- **`internal/ui/model/ui_dialogs.go`** (~980 lines, 28 methods) — central
  dialog dispatch and handling.

- **`internal/ui/dialog/permissions.go`** (~790 lines, 34 methods) — permission
  dialog logic.

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

### Go toolchain version mismatch on Windows

**Symptom.** `go build` and `go test` fail with errors like:

```
compile: version "go1.26.3" does not match go tool version "go1.26.4"
# crypto/internal/boring/sig
# sync/atomic
# internal/cpu
```

`go version` and `go env GOVERSION` report `go1.26.4` (which is what
`go.mod` requires), but the standard library compile target is 1.26.3.

**Root cause.** The default `GOROOT` on this host points to
`C:\Users\16143\.g\go`, which is an old `go1.26.3` install whose
`bin\go.exe` was replaced with the `go1.26.4` binary. The stdlib under
`GOROOT\pkg` and `GOROOT\src` is still 1.26.3, so the 1.26.4 binary
detects the mismatch at compile time. The real `go1.26.4` toolchain
lives at
`C:\Users\16143\go\pkg\mod\golang.org\toolchain@v0.0.1-go1.26.4.windows-amd64\`
(Go's `GOTOOLCHAIN=auto` downloaded it there).

**Canonical fix.** Use the real `go1.26.4` toolchain by setting
`GOROOT` to the toolchain path before any `go` command:

```bash
export GOROOT="C:\\Users\\16143\\go\\pkg\\mod\\golang.org\\toolchain@v0.0.1-go1.26.4.windows-amd64"
# Optional but explicit:
export PATH="C:\\Users\\16143\\go\\pkg\\mod\\golang.org\\toolchain@v0.0.1-go1.26.4.windows-amd64\\bin:$PATH"
go version
# go1.26.4 windows/amd64
go env GOROOT
# C:\Users\16143\go\pkg\mod\golang.org\toolchain@v0.0.1-go1.26.4.windows-amd64
go build -buildvcs=false ./...   # PASS
```

**Verification.** After exporting `GOROOT` as above:
- `go version` → `go1.26.4 windows/amd64`
- `go env GOROOT` → `C:\Users\16143\go\pkg\mod\...toolchain@v0.0.1-go1.26.4...`
- `go build -buildvcs=false ./...` → no output (success)
- `go test -count=1 -short -p=2 -timeout 120s ./...` → packages PASS

**Parallel test limit.** `go test ./...` on the full repo can hit
Windows file-handle / memory limits and surface as `compile: version
does not match` errors. Use `-p=2` (or `-p=1` for very large packages)
to keep parallel compilation manageable on this host.

**Remaining workaround.** Until the default `GOROOT` (`C:\Users\16143\.g\go`)
is re-installed as a clean `go1.26.4`, every shell session that needs
`go build` / `go test` must export the toolchain `GOROOT` first. A
`scripts/go-env.sh` (not yet added) would wrap this for convenience.

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

Before non-trivial TUI changes, read `internal/ui/AGENTS.md`. Optional local
plans may live under `.mocode/plans/` (gitignored). Structure governance
baseline: `docs/dev-notes/structure-governance-baseline.md`.
