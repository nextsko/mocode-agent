# Spec: Agent Toolkit Migration

## Objective
Migrate the LLM-callable tool surface from `internal/core/agent/tools/...` to top-level `tools/...` while preserving the current product behavior, including the recent small behavior changes in built-in and plugin tools.

Success means the top-level toolkit becomes the single implementation home, the agent runtime uses it through an adapter, and tests prove that schemas, validation, permissions, output formatting, metadata, file tracking, LSP notifications, background jobs, and runtime dependencies still behave as expected.

## Non-Negotiable Constraint
This is not a mechanical directory move. Several tools have accumulated subtle behavior changes. Every migrated tool must be reviewed against the current legacy implementation before cutover.

Observed examples:
- `grep`: legacy uses config-driven timeout and regex caches; new toolkit version currently uses a fixed 5s timeout and no cache.
- `todos`: legacy returns hard errors for invalid status; new toolkit returns `ToolResult.Error` for some validation failures and resolves `session.Service` from runtime dependencies.
- `job_*`: migrated toolkit versions mirror core behavior but use toolkit `ToolResult` and manual schemas, so schema and metadata parity must be asserted.
- `bash`: current legacy behavior includes command blockers, attribution/model-specific description template, safe read-only bypass, error categorization, auto-background, TTY, `<cwd>` normalization, output truncation, and background job metadata.
- `edit`/`write`/`multiedit`: current legacy behavior includes read-before-write checks, exact-match errors, CRLF preservation, file history versions, file tracker updates, permission payloads, LSP notifications, diagnostics, and response metadata.
- `view`/`read_files`: current legacy behavior includes skill resource reads, image responses, file size limits, line numbering, UTF-8 validation, suggestions, outside-workdir permissions, and file tracker updates.

## Tech Stack
- Go module: `github.com/package-register/mocode`
- Legacy runtime tool contract: `charm.land/fantasy.AgentTool`
- New toolkit contract: `github.com/package-register/mocode/tools.Tool`
- Agent adapter boundary: `internal/core/agent`
- Tests: Go `testing` + `testify/require`

## Commands
Baseline and verification commands:

```bash
go test ./tools/...
go test ./internal/core/agent/tools/...
go test ./internal/core/agent/...
go build -buildvcs=false ./...
```

For focused slices:

```bash
go test ./tools/builtin/<tool>/... ./internal/core/agent/tools/builtin/<tool>/...
go test ./tools/plugins/<tool>/... ./internal/core/agent/tools/plugins/<tool>/...
```

When full repo tests are expensive on Windows, use:

```bash
go test -count=1 -short -p=2 -timeout 120s ./...
```

## Project Structure
- `tools/`: future framework-agnostic toolkit.
- `tools/builtin/<name>`: built-in filesystem/execution tools.
- `tools/plugins/<name>`: opt-in plugins.
- `tools/builtin/all`, `tools/plugins/all`: blank-import registration umbrellas.
- `internal/core/agent`: adapter from toolkit contract to `fantasy.AgentTool`, runtime dependency injection, agent-owned workflow tools.
- `internal/core/agent/tools`: legacy active tree until cutover; must not be deleted before import graph is clean.
- `.plans/`: execution plan, progress log, findings, this spec, and test matrix.

## Code Style
New toolkit implementations should follow this shape:

```go
type exampleTool struct{}

func New() tools.Tool { return &exampleTool{} }
func (t *exampleTool) Name() string { return ExampleToolName }
func (t *exampleTool) Description() string { return firstLine(exampleDescription) }
func (t *exampleTool) Schema() tools.Schema { /* explicit JSON schema */ }
func (t *exampleTool) Execute(ctx context.Context, tctx tools.ToolContext, args json.RawMessage) (tools.ToolResult, error) {
    var params ExampleParams
    if err := json.Unmarshal(args, &params); err != nil {
        return tools.ErrorResult(err)
    }
    // Preserve legacy validation, permission, output, metadata, and side effects.
}

func init() { tools.Register(ExampleToolName, New()) }
```

Avoid importing `charm.land/fantasy`, `charm.land/catwalk`, `internal/core/agent`, or `internal/core/config` from `tools/...`. Use `tools.RuntimeDependency` for app-owned services.

## Testing Strategy
Every migration slice must add tests before or alongside implementation.

Required coverage per tool:
- Registration: umbrella package registers the tool name.
- Schema: required fields, property names, descriptions, and defaults match the intended public contract.
- Validation: missing required params and invalid values match expected user-facing behavior.
- Success output: response content and metadata match legacy behavior where parity is required.
- Error output: tool-call validation errors are observable as failed tool results, not silent success.
- Side effects: file writes, history, tracker, LSP, permission requests, background job manager, or session saves happen as before.
- Dependency absence: tools depending on runtime services fail with clear errors when the service is missing.

Required cross-cutting tests:
- Adapter converts `tools.Schema` to provider/fantasy schema without dropping `required` fields.
- Adapter converts `tools.ToolResult.Error` into an LLM-visible tool error equivalent to legacy errors.
- Runtime dependency set carries every service used by migrated tools.
- No duplicated active tool names at runtime.
- `tools/...` dependency boundary does not import forbidden packages.

## Boundaries
Always:
- Preserve existing tool names, argument names, and output contracts unless explicitly documented as an intentional behavior change.
- Run focused old/new tests after each tool group.
- Keep legacy runtime active until new registry coverage is complete and adapter parity is proven.
- Update `.plans/toolkit-migration-progress.md` after each phase.

Ask first:
- Intentional behavior changes visible to the model/user.
- Removing a legacy tool or renaming a public tool.
- Adding new external dependencies.
- Deleting `internal/core/agent/tools` before a clean import graph and passing full verification.

Never:
- Mechanically replace legacy tools without parity tests.
- Cut runtime to a partial new registry.
- Let `tools/...` import `internal/core/agent`, `internal/core/config`, `fantasy`, or `catwalk`.
- Treat schema differences, missing required params, or altered metadata as harmless.

## Success Criteria
- `tools/builtin/all` registers all 10 builtins.
- `tools/plugins/all` registers every standard plugin that should be LLM-callable.
- Agent runtime builds tools from the top-level registry via adapter.
- `internal/core/agent/tools` has no remaining production imports and is deleted or reduced to temporary compatibility shims only before final removal.
- Parity/behavior tests cover every high-risk tool group.
- `go test ./tools/...`, `go test ./internal/core/agent/...`, and `go build -buildvcs=false ./...` pass.
- Documentation describes the new layout and how to add a tool.

## Open Questions
- Should `grep` preserve legacy regex cache and config timeout exactly, or should the fixed toolkit timeout be treated as an intentional behavior change? Default: preserve legacy until explicitly changed.
- Should toolkit validation errors consistently return `ToolResult.Error`, while adapter maps them to legacy-style tool errors? Default: yes, but adapter tests must lock the user-visible behavior.
- Should UI-facing tool constants move into `tools` or a small domain package? Default: move stable names into `tools` packages and avoid UI importing legacy agent internals.
