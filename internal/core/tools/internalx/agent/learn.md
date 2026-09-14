Capture a reusable procedure you just worked out as a skill, so the next session starts from it instead of rediscovering it.

Use this after finishing a task where you learned something that is *not obvious*, will recur *across* sessions or projects, and cost real effort to figure out — a debugging trail, a CLI incantation, a build/release sequence, a gotcha with a specific tool. Creating a skill is how a one-off investigation turns into a durable capability, and refining one is how it stays accurate.

## Do not use this

- **One-off solutions** — if the situation cannot recur, a skill is dead weight.
- **Standard, already-documented practice** — read the docs instead of restating them.
- **Project-specific conventions** — those belong in `AGENTS.md` / `CLAUDE.md`, which load automatically.
- **Purely mechanical constraints** — if a linter or a script can enforce it, write the script.
- **Secrets, tokens, credentials or customer data** — never put these in a skill file.

When in doubt, prefer a smaller skill. A skill that is never loaded costs nothing; a wrong one misleads.

## Actions

- `list` — survey the skills that already exist. Do this first when you are unsure whether to create or refine: it tells you which names are taken and which skills are editable.
- `create` — write a brand-new skill. Requires `name`, `description` and `instructions`.
- `refine` — rewrite the body of an existing editable skill, preserving its provenance and backing up the previous version. Requires `name` and `instructions`; pass `description` only when the trigger condition changed.

Bundled skills cannot be modified, and `create` refuses a name that already exists — pick a distinct name instead. Prefer `refine` over creating a near-duplicate: two skills that overlap will both be loaded and will contradict each other.

## House style

These rules are what make a skill findable and worth following.

- **`name`** — kebab-case, verb-first, letters/digits/single hyphens only: `condition-based-waiting`, not `async_test_helpers_v2`. The name must match the skill's directory, which this tool handles for you.
- **`description`** — one line starting with `Use when`. Describe *the trigger and the symptom*, never the procedure. If the description summarises the steps, the model will follow the summary and skip the body, which is exactly backwards.
- **`instructions`** — the full markdown body. Make it the procedure a competent agent could follow cold:
  - what the situation looks like (symptoms, error messages, synonyms)
  - the concrete steps, with real commands and exact flags
  - the pitfalls and the failure modes you actually hit, including how to tell you are on the wrong track
  - how to verify it worked
- Write commands, not prose about commands. Prefer "run `X` and expect `Y`" over "you should run the build".

## Example body

```markdown
# Fixing flaky Go tests caused by shared temp dirs

## When this happens

Test passes alone, fails with `-race` or in CI with `no such file or directory`.

## Steps

1. Reproduce: `go test -race -count=10 ./pkg/...`
2. Find the shared path: `grep -rn "os.TempDir()" --include=*.go .`
3. Replace with `t.TempDir()`, which is per-test and auto-cleaned.

## Pitfalls

- `t.TempDir()` is unavailable in `TestMain`; use `os.MkdirTemp` plus a deferred remove.
- Calling `t.Parallel()` still races if the path is a package-level var.

## Verify

`go test -race -count=50 ./pkg/...` passes.
```

## What happens to what you write

The skill lands in the workspace's user skills directory, and each write is recorded with provenance metadata (`origin`, `revision`, timestamps) plus a backup of the previous version when overwriting. Nothing is lost, and skills you or a teammate wrote by hand are never rewritten into agent-authored ones.

New and updated skills join `<available_skills>` from the **next session** onwards — the current session's list is already loaded, so do not expect to see your new skill until then.
