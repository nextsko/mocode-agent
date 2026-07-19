# Domain-Expert Dispatch Prompt Template

A dispatch prompt describes one task for one named specialist subagent. It is the subagent's entire world — it has no other context. Every section below is load-bearing; omitting one is the most common cause of a wasted subagent run.

## The seven required sections

```
You are the {emoji} **{Role Name}** subagent. {One sentence on the role's specialty and how it fits the project.}

## Verified environment (do NOT re-detect — trust these)
{Paste the locked values from Phase 0: exact paths, versions, target triples, IDs.
 Tell the subagent explicitly not to re-run detection — divergence here wastes the run.}

## Your scope
- **In scope:** {exact files to create/modify, what to produce}
- **Out of scope:** {what they must NOT touch — prevents a subagent from
  sprawling into a neighbor's files}

## Locked interfaces (build to these exact names/signatures)
- Consumes: {what this task uses from earlier tasks — exact names}
- Produces: {what later tasks rely on — exact function names, signatures,
  return types. A subagent working a parallel task sees only its own prompt;
  this block is how the two stays in sync.}

## Steps
{Numbered, bite-sized. Each code-touching step shows the actual code.
 Each command step shows the exact command and expected output. Copied
 from the plan — don't paraphrase.}

## Known failure modes & fixes
{If a step is fragile, say so and give the workaround. This is the
 highest-leverage section: a pre-baked fix turns a 30-minute spiral
 into a 2-minute detour. E.g. "Step 3 will fail at the symlink step
 with 'SeCreateSymbolicLinkPrivilege'. That's expected — then copy the
 .so manually and run gradle with -x rustBuild*."}

## Acceptance — report back ALL of:
{Concrete, checkable items. Each should be something you (the controller)
 will independently verify. Phrase as "report the exact output of X",
 not "confirm it works".}

Be tenacious: {one line on persistence — e.g. "the first attempt at step N
 often has one solvable snag; diagnose from the real error text and retry."}
```

## Annotations on why each section exists

**Role identity.** A persona ("🦀 Rust Core Engineer") focuses decisions and makes the dispatch read as instructions to a specialist, not a generic worker. Distinct roles per subagent is a user-facing requirement of this skill.

**Verified environment.** Subagents left to "detect the environment" will use slightly different paths/versions than you intend, then everything downstream breaks opaquely. Lock the values; forbid re-detection.

**Scope in AND out.** The "out of scope" line prevents the single most common parallel-work disaster: two subagents editing the same file. If two subagents would touch the same files, they cannot run in parallel — sequence them or merge the roles.

**Locked interfaces.** This is the mechanism that lets Phase B run in parallel. Decide the cross-task names and signatures in the plan; paste them into both dispatches. If Rust task produces `greet(name: &str) -> String` and frontend task calls `invoke('greet', {name})`, they merge without coordination.

**Concrete steps with real code.** A subagent reading "implement the feature" will build something plausible but wrong. Show the code. Show the commands. Show expected output. Bite-sized steps also make failures localize to one step.

**Known failure modes.** You (the controller) often know a step is fragile because you've seen it fail, or because Phase 0 surfaced a risk. Passing the workaround in the prompt saves a full subagent retry cycle. This is where hard-won session knowledge gets injected into dispatches.

**Acceptance contract.** Ask for *evidence* (command output, file paths, artifact contents), not *claims* ("confirm it works"). You'll re-verify each item independently in Phase 3 anyway — the contract is also your verification checklist.

**Persistence nudge.** Subagents that give up at the first error and report BLOCKED waste a dispatch. Tell them which step is expected to be hard and that retrying is the job.

## Anti-patterns

- **Pasting session history** ("state after Tasks 1-3...") into a dispatch. A fresh subagent needs its task, its interfaces, and the environment. Not your session narrative.
- **Vague acceptance** ("make sure it works"). You can't verify that. Ask for specific output.
- **Asking the subagent to re-detect** ("find the JDK"). You already know. Tell them.
- **Pre-judging results** ("this should pass easily"). Let them report honestly; you verify.
- **One giant step** ("build the app"). Break it up so failures localize.
