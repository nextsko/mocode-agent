# Mocode Roundtable Team Mode

Roundtable team mode lets multiple specialist agents collaborate on a decision
or plan within a single tool call. A moderator facilitates, specialists
contribute, and optional executors carry out an approved plan.

## How it works

1. The user (or the active agent) invokes the `roundtable` tool with a topic.
2. The coordinator creates a dedicated roundtable session under the current
   session and spins up per-seat sub-sessions for each participant.
3. Participants take turns according to a round-robin schedule.
4. During discussion, seats may only use read-only tools.
5. Any seat can propose a formal motion; votes are collected until consensus
   is reached or the motion fails.
6. If the motion is approved and executors are configured, the meeting moves
   to the execution phase where executor seats may use write tools.
7. The final transcript and any consensus are persisted under
   `.mocode/roundtables/<id>.json` and returned as the tool result.

## Formal syntax for seats

Seats generate normal chat by default. To drive the meeting, a seat can emit
one of these exact prefixes:

- `MOTION: conclude <summary>` — propose ending the meeting with a decision.
- `MOTION: propose_plan <summary> | {"key":"value"}` — propose a plan with an
  optional JSON payload.
- `VOTE: yes|no|abstain [reason]` — vote on the most recent motion.

## Tool parameters

```json
{
  "topic": "Refactor the auth module",
  "participants": [
    {"name": "Moderator", "agent_id": "plan", "role": "moderator"},
    {"name": "Researcher", "agent_id": "task"},
    {"name": "Reviewer", "agent_id": "coder"},
    {"name": "Executor", "agent_id": "coder", "can_execute": true}
  ],
  "max_turns": 20,
  "resume_id": ""
}
```

- `topic` (required unless `resume_id` is set): the meeting subject.
- `participants`: list of seats. When omitted, a default moderator/researcher/
  reviewer/executor panel is used.
- `max_turns`: discussion turn budget (default 20).
- `resume_id`: resume a previously saved roundtable snapshot.

## Enabling the tool

The `roundtable` tool is available to any agent whose `AllowedTools` includes
`roundtable`. The default `coder` agent includes it. To enable it for a custom
agent, add `"roundtable"` to its `AllowedTools` in `Mocode.json` or a
`modes.toml` file.

## Files and persistence

- Snapshots: `.mocode/roundtables/<roundtable-id>.json`
- Per-seat sub-sessions: `.mocode/sessions/<roundtable-session-id>/...`
- Costs from each seat are rolled up into the roundtable session, then into the
  parent session.

## Pub/sub events

The coordinator publishes two notification types during a roundtable:

- `roundtable_turn` — emitted after each successful participant turn.
- `roundtable_finished` — emitted when the meeting ends.

The TUI updates the status line and shows a desktop notification when a
roundtable finishes.

## Testing

Run the roundtable tests:

```bash
go test ./internal/agent/roundtable/... ./internal/agent/ -count=1
```

The coordinator integration tests use a mock `seatRunner` to simulate
participants and verify the turn loop, persistence, consensus, execution,
loop detection, and error handling.
