Record what this session learned, so it survives the session.

Memory is markdown files on disk, in two tiers. **Session memory** belongs to the session that wrote it. **Global memory** is durable knowledge that outlives any one session and is worth carrying into unrelated work. A single index at `mocode://memory/` records where every session's memory lives, so what one session learned is findable from any other.

## When to write

Write when something would be expensive to rediscover and is not already captured:

- a non-obvious root cause, with the symptom that identifies it
- a command, flag or config that actually worked after others did not
- a decision and the reason, when the reason is not visible in the code
- a correction: something you believed, and what showed it was wrong

Do not write: routine progress, restatements of code you just read, or anything already in the repository. A memory that duplicates what is on disk is a memory that will drift out of date.

## Actions

- `write` — store an entry. Defaults to `scope=session`.
- `list` — recent entries, newest first. Pass `scope` to narrow.
- `search` — find entries by text across titles, tags and bodies. Search before writing: an existing entry you can extend beats a near-duplicate.
- `extract` — promote a session memory into the global tier.
- `forget` — archive an entry. Nothing is deleted; the file moves to `.archive/`.

## Two tiers, and why extraction exists

Write to the **session** tier by default. It is the safe direction: promoting later is easy, unpromoting is not.

Promote with `extract` once something has proven generally true — it helped more than once, or it would help in a different project. Extraction **copies rather than moves**, so the session keeps its own record of what happened, and the promoted entry records where it came from (`promoted_from`). Running `extract` twice on the same entry does not duplicate it.

## Writing a good entry

`title` is what the index shows and what a future session searches, so make it specific enough to be findable: `Tachyon rejects the default User-Agent` beats `fixed 403`.

`body` is markdown. Include the concrete detail that would otherwise be lost — the actual error text, the exact command, why the obvious approach failed. `tags` are free-form labels (`["go","testing"]`) and are matched by `search`.

```jsonc
{"action":"write",
 "kind":"lesson",
 "title":"Flaky Go tests caused by a shared temp dir",
 "tags":["go","testing"],
 "body":"Symptom: passes alone, fails under -race or in CI with 'no such file'.\n\nCause: a package-level os.TempDir() shared by parallel tests.\n\nFix: t.TempDir(), which is per-test and auto-cleaned. Watch out: it is unavailable in TestMain."}
```

## Reading memory

`view mocode://memory/` shows the index — every entry, plus where each session's memory lives. Entries are readable individually at their `path` (for example `mocode://memory/global/20260914T120000-1a2b3c4d-flaky-test.md`). The index is regenerated on every change, so it never lags what is on disk.

## Boundaries

- Memory is stored under mocode's own data directory (`~/.local/share/mocode/memories` on Linux, `%LOCALAPPDATA%\mocode\memories` on Windows); `options.memory_dir` moves it.
- Writes are not gated by a permission prompt. They are frequent by design, they land in a mocode-owned directory, and `forget` archives rather than deletes. To turn the tool off entirely, add `memory` to `options.disabled_tools`.
- Memory is not a scratchpad: use `todos` for the current task's steps and `think` for reasoning. Memory is for what should outlive the task.
- Do not store secrets, tokens or credentials.
