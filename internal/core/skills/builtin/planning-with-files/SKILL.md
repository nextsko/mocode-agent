---
name: planning-with-files
description: >-
  Use when the user has a multi-step task that needs structured planning,
  progress tracking, and context management across sessions. Covers creating
  task plans with phases/stages, tracking progress with checkpoints, saving
  research findings and decisions, maintaining execution context across
  interruptions, and managing the plan-implement-verify lifecycle. Essential
  for complex tasks that span multiple conversations or require careful
  step-by-step execution with review checkpoints.
---

# Planning with Files

Structured task planning with persistent progress tracking.

This is a **meta-level process skill** for managing complex, multi-step tasks. It creates three files to track all context, preventing loss across sessions.

## The Three Core Files

When starting a complex task, create these files in a `.plans/` directory:

```
.plans/
├── task_plan.md     # What to do and how
├── progress.md      # What's been done and what's next
└── findings.md      # What's been learned and decided
```

---

## 1. task_plan.md — The Plan

Defines the overall mission, broken into phases.

```markdown
# Task Plan: [Task Name]

## Goal
[One sentence describing the final state]

## Current Phase
Phase 1

## Phases

### Phase 1: [Name]
- [ ] [Specific action item]
- [ ] [Specific action item]
- **Status:** in_progress

### Phase 2: [Name]
- [ ] [Specific action item]
- **Status:** pending

## Key Questions
1. [Question to answer during execution]
2. [Question to answer during execution]

## Decision Log
| Decision | Rationale | Date |
|----------|-----------|------|
|          |           |      |
```

## 2. progress.md — Progress Tracker

Tracks execution progress and enables **context reboot** across sessions.

```markdown
# Execution Progress

## Session: [Date]

### Phase 1: [Name]
- **Status:** in_progress
- **Started:** [timestamp]
- Operations performed:
  - [operation 1]
  - [operation 2]
- Files created/modified:
  - path/to/file

## Test Results
| Test | Input | Expected | Actual | Status |
|------|-------|----------|--------|--------|
|      |       |          |        |        |

## Error Log
| Time | Error | Attempts | Solution |
|------|-------|----------|----------|
|      |       | 1        |          |

## 5-Question Reboot Check
<!-- Answer these to resume context after interruption -->
| Question | Answer |
|----------|--------|
| Where am I? | Phase X |
| Where am I going? | Remaining phases |
| What is the goal? | [Goal from task_plan] |
| What have I learned? | See findings.md |
| What have I done? | See progress log above |
```

> **The 5-Question Reboot Check** is the key innovation. After any interruption (session timeout, context reset, etc.), answering these 5 questions from the files allows immediate resumption without re-reading the entire conversation.

## 3. findings.md — Research & Decisions

Captures all discoveries, preventing re-work and context loss.

```markdown
# Research Findings & Decisions

## Requirements
<!-- Extracted from user request -->
-

## Source Files
<!-- Paths, types, key attributes -->
-

## Field Mapping
| Source Field | Target Field | Notes |
|-------------|-------------|-------|
|             |             |       |

## Key Findings
<!-- Update after every 2-3 tool calls -->
-

## Technical Decisions
| Decision | Rationale |
|----------|-----------|
|          |           |

## Issues Encountered
| Issue | Solution |
|-------|----------|
|       |          |

## Reference Resources
<!-- URLs, file paths, API docs -->
-
```

---

## Workflow

```
1. INIT — Understand requirements → Create 3 files in .plans/
2. EXECUTE — Work through phases one at a time
3. SAVE — After every 2-3 tool calls, update findings.md and progress.md
4. REVIEW — At phase boundaries, verify progress before continuing
5. REBOOT — If interrupted, read the 3 files and answer the 5 questions
6. COMPLETE — Mark all phases done, deliver results, clean up
```

## Phase Transition Rules

- **Phase N → N+1**: Only when all items in Phase N are checked
- **Blocked phase**: Document the blocker in findings.md, discuss with user
- **Rollback**: If a phase needs rework, update task_plan.md and notes in progress.md
- **New findings**: If a discovery changes the plan, update task_plan.md and log the decision in findings.md

## Key Discipline

> **After every 2-3 tool calls, save to findings.md.**
>
> Multimodal content (images, browser results) must be immediately transcribed to text or it will be lost on context reset.

## When to Use This Skill

- Task requires 3+ sequential steps
- Task involves research/investigation before implementation
- Task spans multiple files or systems
- Task might be interrupted (timeout, context limit)
- User says "plan it out first" or "make a plan"
- User provides a numbered list of requirements
- The task requires review checkpoints

## When NOT to Use This Skill

- Single-step tasks (one file change, one command)
- Simple Q&A or information lookup
- Trivial operations under 30 seconds
