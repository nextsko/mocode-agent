---
name: multi-agent-orchestration
description: Deliver an end-to-end objective by orchestrating multiple named domain-expert subagents across phased work, when the user hands you full autonomy ("just get it done", "you make all the decisions", "I only want results"). Use whenever a goal spans several distinct specialties (e.g. environment setup + implementation + QA), the user delegates all decisions to you, and the work decomposes into phases with clean interfaces between them. Distinct from subagent-driven-development (one generic implementer per planned task + review gate) and dispatching-parallel-agents (fan-out for independent debugging).
---

# Multi-Agent Orchestration

## What this skill is

You act as the **orchestrator-controller**. You hold the plan, the cross-task interfaces, and the verification bar. You never do the domain work yourself — you dispatch it to a sequence of **named domain-expert subagents**, each a distinct persona with a clear specialty. Between phases you verify the real state of the world (files, command output, artifacts) rather than trusting subagent self-reports.

This is for **full-autonomy delivery**: the user said "you decide everything, I just want the result." So you decide, execute end-to-end, and hand back verified artifacts.

## When to use (and when NOT)

**Use when ALL hold:**
- The user delegates decisions to you ("just do it", "全面推进", "我只要结果").
- The objective spans 3+ distinct specialties (e.g. infra + backend + frontend + QA).
- The work decomposes into **phases** with interfaces you can lock up front.
- You can verify outputs concretely (files exist, commands succeed, artifacts render).

**Do NOT use when:**
- The user wants to approve each step — use subagent-driven-development or inline work with checkpoints.
- Tasks are tightly coupled with no clean interfaces — do them inline or break the spec first.
- It's a single-domain task one subagent can own end-to-end — just dispatch one.
- You can't verify outputs (pure judgment calls, subjective quality) — the controller-led verification loop is load-bearing; without it the pattern degrades into trusting reports.

**vs `subagent-driven-development`:** that skill runs one *generic* implementer per pre-written plan task, then a review gate per task, in a linear sequence. This skill runs *named specialist* subagents (each a different role), in **phases** (parallel within a phase, sequential across phases), and the controller verifies by inspecting artifacts directly — no separate reviewer subagent. Use that one when you have a detailed plan and want TDD + per-task review; use this one when you're driving an open-ended objective end-to-end with full autonomy.

**vs `dispatching-parallel-agents`:** that's fan-out for N independent debugging problems with no phasing or roles. This is phased delivery with role specialization and a controller.

## The pattern, end to end

### Phase 0 — Reconnaissance (you, no subagents)

Before dispatching anything, establish ground truth yourself. Do not delegate discovery — a subagent dispatched with wrong environment assumptions wastes a whole run.

- Detect versions, paths, installed tools directly with shell commands.
- Record **locked values** (exact paths, versions, target triples) — every later dispatch references these verbatim so subagents never re-detect and diverge.
- Note what's **missing** and will need provisioning (a subagent's job).
- Check for **risks** that decide architecture: e.g. "bundled Gradle needs JDK ≤21" determines feasibility of a whole approach.

Output of Phase 0: a short environment table you'll paste into the plan and into every relevant dispatch.

### Phase 1 — Plan with phases, roles, and locked interfaces

Write a plan with this structure (use the `writing-plans` skill for the document mechanics):

1. **Goal** — one sentence, concrete and verifiable.
2. **Architecture** — 2-3 sentences on approach.
3. **Verified environment table** — Phase 0's locked values, copied verbatim.
4. **Decisions made** — record the key choices *and why*, so a subagent (or future you) doesn't re-litigate them.
5. **Named subagent roles** — one line each, distinct specialties. (See "Designing roles" below.)
6. **Phasing** — which tasks run in parallel (same phase) vs sequentially (later phase depends on earlier). This is the dependency graph.
7. **Per-task blocks**, each with: Files (create/modify with exact paths), Interfaces (Consumes from earlier tasks / Produces for later), bite-sized steps with exact commands and expected output.
8. **Verification gates** — per task AND a final acceptance gate. Each gate is a concrete check (command output, file existence, artifact content), never a feeling.

**Lock the interfaces.** The single biggest lever: decide the exact names, signatures, and types crossing task boundaries *in the plan*, before anyone implements. Then two subagents working different files in parallel cannot drift. Example: "Task 3 produces `fn greet(name: &str) -> String`; Task 4 calls `invoke('greet', { name: 'X' })`." If those match, parallel work merges cleanly.

### Phase 2 — Dispatch, phase by phase

Within a phase, dispatch independent subagents **in one message** (multiple Agent tool calls in the same response = parallel). Across phases, wait for the prior phase to verify before starting the next.

**Every dispatch prompt is self-contained** — a fresh subagent has zero context. Each prompt must carry:
1. **Role identity** — "You are the 🦀 Rust Core Engineer subagent." Give them a persona; it focuses their decisions.
2. **Verified environment** — paste the locked values. Tell them "do NOT re-detect; trust these."
3. **Their exact scope** — files to touch, what to produce. State what's *out* of scope ("Do NOT touch frontend files").
4. **Locked interfaces** — the exact names/signatures they consume and produce.
5. **Concrete steps** with exact commands and expected output (from the plan).
6. **The acceptance contract** — exactly what they must report back, and that you will verify it independently.
7. **Known failure modes + fixes** — if you know a step is fragile (e.g. "this command will fail at the symlink step, that's expected — then do X"), say so. This is the highest-leverage thing you can pass to a subagent: a pre-baked workaround turns a 30-minute spiral into a 2-minute detour.

**Model selection:** use cheap/fast models for mechanical, well-specified tasks (transcription, single-file edits with complete code in the prompt); standard models for multi-file integration; the most capable model for tasks needing judgment or broad context. Always specify the model explicitly — an omitted model inherits your session's model and silently inflates cost.

### Phase 3 — Controller-led verification (between phases, and at the end)

This is the load-bearing step. **Never mark a phase done on a subagent's self-report.** Subagents will confidently claim success, misread their own output, or report a slightly-wrong value (e.g. an emoji rendered as a different emoji in their dump). Verify yourself:

- Run the verification commands yourself, against the real filesystem/devices.
- When a subagent claims "the output shows X", read the artifact directly (`uiautomator` dump, log file, screenshot) and confirm the literal text — subagents mis-OCR and mis-grep.
- Watch for **discrepancies** between the report and reality. Investigate them; they're either a reporting bug (harmless) or a real defect (must fix). Don't assume which.
- Only when your independent check passes does the phase complete and the next phase start.

**Final acceptance:** build a prompt-to-artifact checklist from the user's objective. Map every requirement to concrete evidence. Inspect each. A passing test suite or a "build succeeded" is a proxy signal, not completion — it must *cover* the requirement. Treat uncertainty as not-done.

### Phase 4 — Hand back

Report to the user: what was delivered (artifact paths), the independent verification evidence (what you checked, not what subagents claimed), decisions you made on their behalf, and any durable caveats (e.g. "future builds need Windows Developer Mode").

## Designing roles

Each subagent is a **different** domain expert. Distinct personas focus the work and make the dispatches read differently. Roles are assigned per-phase from the objective's natural specialties. Examples (pick what fits the objective — don't force these):

- 🏗️ **Environment/Infra Ops Engineer** — provisioning, emulators/containers, env vars, booting services. Goes first; others depend on the environment.
- 🧰 **Project Architect** — scaffolding, project generation, wiring.
- 🦀 / 🔧 / 🎨 **Domain Implementers** — one per distinct technical area (Rust core, frontend UI, DB schema, API layer...). Keep their file sets disjoint so they can run in parallel.
- 🚀 **Build/Deploy/QA Engineer** — packaging, deployment, capturing acceptance evidence. Goes last; needs everything before it.
- 🛠️ **Reliability Engineer** — scripting fragile workflows into reproducible automation (a follow-up phase once a manual workaround is proven).
- 📱 **Release Engineer** — signing, distribution formats, multi-target builds.
- 📚 **Documentation Architect** — final docs, verified against real files.

**Rule of thumb:** if two subagents would touch the same files, they cannot run in parallel — either sequence them or merge the roles.

## Durable artifacts as the recovery map

If the session compacts, conversation memory evaporates. The controller must be able to resume from durable state:

- The **plan file** on disk is the source of truth for what's being built.
- A **todo list** tracks phase/task status.
- For long efforts, a **progress ledger file** (one line per completed task: "Phase 2 Task 3: done, artifact at <path>, verified <how>") survives compaction. After compaction, read the ledger and the filesystem — trust them over your own recollection.

## Narration and pacing

Between dispatches, narrate at most one short line per action. The plan, the todo list, and the verification output carry the record — don't summarize endlessly.

**Continuous execution:** do not pause to ask "should I continue?" between phases. The user delegated full autonomy — execute end-to-end. Stop only for: a blocker you genuinely cannot resolve, an ambiguity that prevents progress, or completion. The exception is a decision that is *the user's to make* (irreversible, outward-facing, or genuinely a preference) — surface those with `AskUserQuestion` rather than guessing.

## Common failure modes (and how this skill prevents them)

1. **Subagents re-detect environment and diverge.** → Phase 0 locks values; every dispatch pastes them with "do not re-detect."
2. **Parallel subagents produce incompatible interfaces.** → Interfaces are decided in the plan before anyone codes.
3. **Controller trusts a false success report.** → Phase 3 verifies independently; the controller reads artifacts, not claims.
4. **Controller does domain work inline, polluting its context.** → The controller only orchestrates and verifies; domain work always goes to a subagent.
5. **One fragile step kills a whole subagent run.** → Known failure modes + pre-baked fixes are passed in the dispatch.
6. **Context compaction loses the thread.** → Plan file + todo list + progress ledger are durable; the controller resumes from them.
7. **Over- or under-dispatching.** → One subagent per distinct specialty; merge roles that would touch the same files; never dispatch a subagent for something you can verify in one shell command.

## Worked example (the session that produced this skill)

Objective (full autonomy): "Build a Rust app packaged as an Android APK, get the emulator running, deliver only results. Use subagents as different domain experts."

- **Phase 0:** detected JDK 21, Android SDK+NDK, Rust+4 android targets, Node — all present. Noted `cmdline-tools` not under `latest/`, no emulator/AVD.
- **Phase 1 plan:** 5 named roles (🏗️ Emulator Ops, 🧰 Tauri Architect, 🦀 Rust Core, 🎨 Frontend, 🚀 Build/QA). Locked interface: 3 command names + signatures shared between Rust and frontend tasks.
- **Phase 2:**
  - Phase A (parallel): 🏗️ booted emulator ∥ 🧰 scaffolded project.
  - Phase B (parallel): 🦀 wrote commands ∥ 🎨 wrote UI — disjoint file sets, locked interface.
  - Phase C (sequential): 🚀 built APK, deployed, captured evidence. (Followed by 🛠️ Build Reliability + 📱 Release + 📚 Docs in later rounds.)
- **Phase 3 (controller verification):** independently ran `adb devices`, `apksigner verify`, `unzip -l | grep lib/`, and read `uiautomator` XML dumps to confirm the literal on-device text — caught one misreported emoji (crab reported as elephant) by reading the source, confirmed it was a reporting artifact not a defect.
- **Phase 4:** delivered APK paths + independent verification evidence + durable caveats (Windows Developer Mode for future builds).

8 subagents, 8 distinct roles, one controller. Objective delivered and independently verified.

## References

- [references/dispatch-prompt-template.md](references/dispatch-prompt-template.md) — annotated template for a domain-expert dispatch prompt, with the seven required sections.
- [references/verification-checklist.md](references/verification-checklist.md) — how to build the prompt-to-artifact acceptance checklist and what counts as real evidence vs a proxy signal.

## Integration with other skills

- **writing-plans** — use for the plan document mechanics (header, task blocks, bite-sized steps).
- **verification-before-completion** — the philosophy behind Phase 3; this skill operationalizes it for multi-agent delivery.
- **dispatching-parallel-agents** — the parallel-dispatch mechanics (multiple Agent calls in one response); this skill adds phasing, roles, and controller verification on top.
- **subagent-driven-development** — alternative when you have a detailed plan and want per-task TDD + review gates instead of phased role-based delivery.
