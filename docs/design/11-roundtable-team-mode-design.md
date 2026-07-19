# Mocode Roundtable Team Mode — Design Spec

**Status:** implemented  
**Date:** 2026-06-13  
**Scope:** Core engine, coordinator integration, pub/sub events, TUI status updates, persistence, and tests are implemented in `internal/agent/roundtable`, `internal/agent/roundtable_tool.go`, and `internal/ui/model/ui.go`.

---

## 1. Problem & Vision

Mocode already supports single-agent coding sessions and sub-agent delegation (`agent`, `agentic_fetch`, `transfer_to_agent`). What it lacks is a **deliberative team mode**: several specialist agents sitting around a shared table, discussing a task, reaching a decision, and then executing it together.

This spec proposes a **Roundtable Team Mode** for Mocode:

- A user can start a roundtable meeting on a topic.
- A **Moderator** + N **Specialists** take turns speaking.
- All participants share the same ordered transcript.
- The meeting produces a structured decision/plan.
- The plan is handed off to execution agents.
- The meeting is persistent, resumable, and observable in the TUI.

The experience should feel like a terminal-native, cost-conscious version of AutoGen GroupChat combined with the structured output discipline of MetaGPT and the execution handoff model of Cline Teams.

---

## 2. Goals & Non-Goals

### Goals

- Enable multi-agent deliberation inside Mocode without replacing the existing single-agent flow.
- Reuse as much existing infrastructure as possible: `Coordinator`, `SessionAgent`, child sessions, tool registry, pub/sub, SQLite sessions, TUI message list.
- Keep meetings observable, resumable, and budget-aware.
- Produce structured, executable plans rather than open-ended chat logs.

### Non-Goals

- Reimplement a general-purpose workflow engine ( reuse `internal/orchestration/swarm` only if it already fits; do not force it).
- Simulate a full software company with dozens of roles.
- Support unconstrained parallel agent chatter by default.
- Build a separate web UI; the TUI remains the primary surface.

---

## 3. External Research Summary

Six representative systems were analyzed via parallel sub-agents.

| System | Pattern | Adopt for Mocode | Avoid for Mocode |
|---|---|---|---|
| **AutoGen GroupChat** | Shared transcript + GroupChatManager selects next speaker | Shared broadcast transcript; explicit termination and max-round guards | `auto` LLM-driven speaker selection on every turn; unbounded groups |
| **ChatDev** | Waterfall ChatChain with roles (CEO/CTO/Programmer/Tester) | Role-specific prompts; cross-examination checkpoints before phase transitions | Full 4-phase SDLC simulation; company-roleplay overhead |
| **MetaGPT** | SOP-driven agents publish structured documents to a shared message pool | Structured artifacts (JSON plan) over free-form prose; SOP prompts | Heavy document-generation churn; rigid one-way pipeline |
| **Cline Teams** | Coordinator + specialists with persistent team state and task board | Coordinator/specialist separation; persistent team state; terminal-first posture | Opaque sub-agent execution; too many parallel specialists |
| **OpenHands** | Event-sourced runtime with nested delegation and budget/stuck detection | Event sourcing as persistence model; delegation lifecycle; budget/stuck detection | Docker sandbox dependency; over-generalizing into a full agent OS |
| **Roundtable AI** | MCP server that unifies external AI assistants in parallel | Model specialization idea | External MCP-only; not an in-session collaboration primitive |

### Methodology / Best Practices Distilled

1. **Shared transcript is the source of truth.** All participants read the same ordered log. Avoid private scratchpads that diverge.
2. **Deterministic turn policy by default.** Only pay for an LLM to choose the next speaker when the value clearly outweighs the cost.
3. **Separate deliberation from execution.** Meetings decide; executors act. This keeps the transcript focused and the permission model simple.
4. **Use structured motions and decisions.** Free-form consensus extraction is fragile. Formal motions (`propose_plan`, `conclude`, `call_vote`) and explicit votes make termination deterministic.
5. **Read-only tools during discussion.** Prevent a discussion from accidentally mutating the workspace. Writes happen in the execution phase.
6. **Bound context aggressively.** Feed each turn only the original request, active agenda, unresolved questions, and the last K transcript turns.
7. **Human gates at phase transitions.** Let the team run autonomously inside a phase, but require explicit approval to move from planning to execution.
8. **Track token/cost budgets visibly.** Show running spend in the TUI; cap per meeting, per role, and per turn.
9. **Detect loops early.** Hash recent messages/tool-calls and flag repeated proposals, repeated tool invocations, and cycling agent transitions.
10. **Persistence via event sourcing.** Append-only transcript events make resume, audit, and TUI rendering straightforward.

---

## 4. Architecture

### 4.1 Components

```text
┌─────────────────────────────────────────────────────────────────┐
│ User / TUI                                                      │
│  • starts/resumes a roundtable                                  │
│  • observes transcript, roster, decisions, budget               │
│  • approves phase transitions and interrupts                    │
└───────────────────────┬─────────────────────────────────────────┘
                        │ pub/sub RoundtableEvent
┌───────────────────────▼─────────────────────────────────────────┐
│ Coordinator (existing)                                          │
│  • owns the roundtable lifecycle                                │
│  • creates child sessions for each seat                         │
│  • runs the turn loop by calling roundtable.Advance             │
│  • delegates execution to agent/transfer_to_agent               │
└───────────────────────┬─────────────────────────────────────────┘
                        │
┌───────────────────────▼─────────────────────────────────────────┐
│ internal/agent/roundtable (new package)                         │
│  • Roundtable state machine                                     │
│  • transcript, motions, votes, decisions                        │
│  • speaker selection strategies                                 │
│  • consensus extraction                                         │
│  • context summarization / working memory                       │
└───────────────────────┬─────────────────────────────────────────┘
                        │ child SessionAgent runs
┌───────────────────────▼─────────────────────────────────────────┐
│ Seats (Moderator + Specialists)                                 │
│  • each seat = named agent config + child session               │
│  • turn prompt = role prompt + shared context + recent turns    │
└─────────────────────────────────────────────────────────────────┘
```

### 4.2 Integration with Mocode

- **No change to `SessionAgent` or `Coordinator` public interfaces.** The roundtable is an orchestration layer on top.
- **Child sessions** are created through the existing `Coordinator.createChildSession` path; each seat gets its own `SessionAgent` instance and session ID.
- **Tool registry** is reused. During discussion, tools are wrapped in a `ReadOnlyToolGuard`. During execution, normal tools are available (subject to existing permission/hook layers).
- **Pub/sub** emits `RoundtableEvent` structs on the existing broker; the TUI subscribes like it does for agent notifications.
- **Persistence** stores roundtables and transcript events in the existing SQLite DB, alongside sessions/messages.

---

## 5. Roundtable Protocol

### 5.1 Participants

- **Moderator**: chairs the meeting, sets agenda, selects next speaker (under a turn policy), proposes motions, synthesizes decisions.
- **Specialists**: domain experts such as `architect`, `coder`, `reviewer`, `tester`, `security`, `docs`. Each maps to a named agent config in `Mocode.json`.
- **User**: can interrupt, add remarks, approve/reject phase transitions.

### 5.2 Statement / Turn Model

Every turn produces a `Statement` appended to the transcript:

```go
type StatementKind int

const (
    KindChat StatementKind = iota
    KindToolResult
    KindMotion
    KindVote
    KindConsensus
    KindSystem
)

type Statement struct {
    ID           string
    RoundtableID string
    Seq          int
    Speaker      string        // seat name
    Kind         StatementKind
    Content      string        // markdown text
    ToolCalls    []ToolCallRef // optional, read-only during discussion
    Motion       *Motion
    Vote         *StatementVote
    CreatedAt    time.Time
}
```

Motions are formal:

- `propose_plan` — propose a concrete plan.
- `request_clarification` — ask user or another seat for info.
- `call_vote` — open a vote on a prior motion.
- `conclude` — declare the meeting finished with a decision.
- `deadlock` — declare no consensus and stop.

Consensus is extracted by code, not by LLM summarization:

- A `conclude` or `propose_plan` motion is open.
- All non-abstaining participants have voted `yes`.
- A parseable plan payload is present.

### 5.3 Phases

```go
type Phase string

const (
    PhaseOpening    Phase = "opening"
    PhaseDiscussion Phase = "discussion"
    PhasePlanning   Phase = "planning"
    PhaseVoting     Phase = "voting"
    PhaseApproved   Phase = "approved"
    PhaseExecution  Phase = "execution"
    PhaseDone       Phase = "done"
    PhaseInterrupted Phase = "interrupted"
)
```

Default flow: `opening → discussion → planning → voting → approved → execution → done`.

Phase transitions are events that the Moderator can propose and that may require human approval.

### 5.4 Turn Policies

Pluggable speaker selection:

- `round_robin` — deterministic order; default.
- `moderator_selects` — Moderator LLM picks next speaker (opt-in).
- `fixed_agenda` — pre-defined rounds per phase.
- `tag_in` — previous speaker tags the next.

Recommendation: default to `round_robin` with Moderator override for agenda control.

### 5.5 Tool Use During Discussion

Allowed read-only tools (configurable):

- `view`, `read_files`
- `grep`, `glob`
- `fetch` (read-only GET)
- `lsp_diagnostics`, `lsp_references`
- `mocode_info`, `mocode_logs`
- `think`

Denied during discussion:

- `edit`, `multiedit`, `write`
- `bash` (except safe read-only commands if explicitly allowed)
- `agent` (delegation is the Moderator's job)

Tool results are inserted into the transcript as `KindToolResult` so every seat sees them.

---

## 6. Data Model

Recommended hybrid persistence:

```sql
CREATE TABLE teams (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    config_json TEXT NOT NULL, -- members, prompts, default model, max rounds
    created_at DATETIME NOT NULL
);

CREATE TABLE team_sessions (
    id TEXT PRIMARY KEY,
    team_id TEXT REFERENCES teams(id),
    parent_session_id TEXT REFERENCES sessions(id),
    status TEXT NOT NULL, -- active | suspended | completed | abandoned
    title TEXT,
    topic TEXT,
    phase TEXT,
    working_memory_json TEXT,
    summary TEXT,
    token_usage_estimated INT DEFAULT 0,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);

CREATE TABLE transcript_entries (
    id TEXT PRIMARY KEY,
    session_id TEXT REFERENCES team_sessions(id),
    seq INTEGER NOT NULL,
    actor TEXT NOT NULL, -- seat name or "system"
    entry_type TEXT NOT NULL, -- utterance | tool_call | decision | checkpoint | system
    payload_json TEXT NOT NULL,
    created_at DATETIME NOT NULL
);

CREATE TABLE decisions (
    id TEXT PRIMARY KEY,
    session_id TEXT REFERENCES team_sessions(id),
    seq INTEGER NOT NULL,
    title TEXT,
    body TEXT,
    plan_json TEXT,
    voting_record_json TEXT,
    created_at DATETIME NOT NULL
);

CREATE TABLE task_board_items (
    id TEXT PRIMARY KEY,
    session_id TEXT REFERENCES team_sessions(id),
    seq INTEGER NOT NULL,
    title TEXT,
    description TEXT,
    assignee TEXT,
    status TEXT, -- proposed | accepted | in_progress | done | rejected
    source_decision_id TEXT REFERENCES decisions(id),
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);
```

Resume semantics:

1. Load `team_sessions` row + `working_memory_json`.
2. Optionally load last K transcript entries for TUI display.
3. Continue the turn loop from the saved phase.
4. New entries append with monotonically increasing `seq`.

Never replay the full transcript into the LLM context on resume.

---

## 7. Guardrails

### 7.1 Budgets

```go
type MeetingBudget struct {
    MaxInputTokens      int
    MaxOutputTokens     int
    MaxTotalTokens      int
    MaxCostCents        int
    PerRoleMaxTokens    map[string]int
}
```

Defaults (tunable):

- 64k input / 16k output per meeting.
- 20 turns max.
- 1 parallel LLM call, 2 parallel specialists, 4 parallel tool calls.

### 7.2 Approval Gates

Default policy:

- `discussion → planning` — Moderator decides.
- `planning → voting` — Moderator proposes; human approval optional.
- `voting → approved` — always human approval.
- `approved → execution` — always human approval.
- State-modifying tools during `execution` — follow existing permission/hook rules.

### 7.3 Loop Detection

Detect:

- Repeated identical or near-identical proposals within a hash window.
- Repeated tool calls with the same arguments.
- Cycling agent transitions (A → B → A → B).

Action severity: `warn` → `pause` → `stop`. On pause, Moderator proposes recovery or deadlock.

### 7.4 Permission Model

- Each Specialist inherits the base tool allow-list from its named agent config.
- Discussion phase overlays `ReadOnlyToolGuard`.
- Existing `PreToolUse` hooks run per tool call unless configured otherwise.

---

## 8. TUI / UX

- **Transcript** renders in the existing chat area as `RoundtableMessageItem`s with seat badges, turn index, and phase tag.
- **Side panel** (reuse `internal/ui/panel`) shows:
  - Seat roster + current speaker + status.
  - Active agenda items.
  - Adopted decisions.
  - Meeting phase pill and budget bar.
- **Interrupts** appear as highlighted inline messages; urgent ones open a modal dialog.
- **Tool calls** during discussion nest under the turn item like existing tool-call trees.
- **Events** flow through pub/sub; no polling.

Entry points:

- Slash command: `/roundtable "design retry backoff for MCP client"`
- Resume: `/roundtable resume <session-id>`
- Agent tool: current agent can call `start_roundtable`.

---

## 9. Default Specialist Roster (v1)

| Role | Responsibility | Maps to agent |
|---|---|---|
| `moderator` | Chairs meeting, synthesizes, calls votes | existing `task` or new `team_lead` |
| `architect` | High-level design, tech choices, file plan | new mode template |
| `coder` | Implementation approach, risk estimate | existing `coder` |
| `reviewer` | Bugs, style, security, test gaps | new mode template |
| `tester` | Test plan, edge cases | new mode template |

Users can customize the roster in `Mocode.json` under a new `teams` section.

---

## 10. Open Questions

1. Should the Moderator be a separate agent definition or can an existing agent double as Moderator?
2. Should execution happen in the parent session, or spawn dedicated child executor sessions?
3. Should the user be a silent observer, an interrupt-only approver, or a full participant with turns?
4. Should roundtable events live in the existing `message` table with metadata, or stay in dedicated tables?
5. Should meeting budgets draw from the parent-session budget or be independent caps?
6. Should we reuse `internal/orchestration/swarm` types, or keep the runtime self-contained?

---

## 11. Next Step

After this spec is approved, invoke the `writing-plans` skill to produce an incremental implementation plan.
