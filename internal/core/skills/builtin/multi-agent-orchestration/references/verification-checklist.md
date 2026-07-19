# Controller-Led Verification Checklist

Phase 3 of multi-agent-orchestration is the difference between "the subagents reported success" and "the objective is actually met." Subagents misread their own output, confidently report slightly-wrong values, and sometimes straight-up hallucinate success. You verify, independently, every time.

## Build the checklist from the objective, not from what was done

Before declaring done, restate the user's objective as a list of concrete deliverables. For each, write down the **specific evidence** you will inspect. The evidence is a file path, a command's output, or an artifact's content — never a subagent's claim.

Template:

```
Objective requirement            | Evidence to inspect           | Checked?
--------------------------------|-------------------------------|--------
"build an APK"                  | <path>.apk exists, size > 1MB | [ ]
"runs on the emulator"          | adb get-state = device;       | [ ]
                                | sys.boot_completed = 1;       | [ ]
                                | pm list packages shows <id>   | [ ]
"Rust bridge works"             | uiautomator dump #out node    | [ ]
                                | contains literal <expected>   | [ ]
"signed for distribution"       | apksigner verify -> Verifies  | [ ]
```

Build this from the user's *words*, not from the subagents' task lists. A subagent completing its task is not the same as the objective being met — the task is a proxy; the objective is the bar.

## What counts as real evidence (and what doesn't)

**Real evidence:**
- You ran the command yourself and read the output.
- You read the file and saw the literal bytes/text you needed.
- A machine-readable artifact (XML dump, JSON, log line) contains the exact expected string.
- A tool whose job is to verify (`apksigner verify`, a test runner, a type checker) returned success.

**Proxy signals — useful but not sufficient alone:**
- "Build succeeded" — the build can succeed without the output meeting the requirement (wrong arch, missing lib, unsigned).
- A test suite passing — only counts if the tests *cover* the requirement. A passing suite that doesn't test the requirement proves nothing.
- A subagent's report — always re-verify the load-bearing claims.
- A screenshot existing — a 0-byte or all-black screenshot "exists." Check size and content.
- Elapsed effort / many tool calls — activity is not achievement.

When a requirement is met only by proxy signals, that's a gap. Either find real evidence or treat it as not-done.

## Reading artifacts directly (the key technique)

When a subagent says "the UI shows X", do not trust it. Read the artifact:

- **On-device UI text:** `adb shell uiautomator dump` (watch for path-mangling in Git Bash — `export MSYS_NO_PATHCONV=1` and use `adb exec-out`/`adb shell cat` to pull the file), then grep the XML for the node's `text=` attribute. This is machine-readable ground truth; a subagent's "I see X" is often a misread.
- **File contents:** read them yourself rather than trusting "I wrote X."
- **APK contents:** `unzip -l <apk> | grep lib/` to confirm the native lib and its ABI are actually packaged.
- **Signatures:** run `apksigner verify --print-certs` yourself; don't trust "it's signed."
- **Process/device state:** run `adb devices`, `getprop`, `pm list packages` yourself.

## Investigate discrepancies, don't explain them away

When a subagent's report disagrees with reality, one of three things is true:
1. **Reporting bug** — the subagent misread its own output (e.g. an emoji entity decoded differently). Usually harmless once you confirm the underlying thing is fine.
2. **Real defect** — the subagent claimed success but the artifact is actually wrong. Must fix.
3. **Wrong artifact mapping** — the subagent captured evidence for the wrong thing (e.g. screenshot filenames didn't match the action order). Re-derive from raw artifacts.

Never assume which. Read the source. Example from the session that produced this skill: a subagent reported the on-device text as `🐘` (elephant) when the Rust source had `🦀` (crab). Reading `lib.rs` confirmed the source was correct; the discrepancy was a reporting artifact (HTML entity decoding in the dump). Confirmed harmless. But the only way to know was to read the source.

## Don't accept a green light on a requirement the evidence doesn't cover

The trap: a subagent runs a test suite, it passes, the controller marks the requirement done. But does the suite *test the requirement*? A "build succeeded" doesn't prove the APK runs. A "cargo check passed" doesn't prove the command output is correct on-device. For each requirement, ask: "does the evidence I have actually exercise this requirement?" If not, get better evidence before declaring done.

## Treat uncertainty as not-done

If you can't tell whether a requirement is met, it isn't. Do more verification, dispatch a fix, or escalate — don't round up to "probably fine." The runtime's completion audit will check; your job is to make sure it passes.

## Final acceptance gate

Before handing back to the user:
1. Every row in the checklist has real evidence (not proxy-only).
2. Every discrepancy between subagent reports and reality has been resolved.
3. You've stated, plainly, what you verified and how — so the user can see the evidence chain, not just the conclusion.
4. You've listed durable caveats (things that will bite the user later: "future builds need Developer Mode", "the keystore password is X, rotate before shipping").
