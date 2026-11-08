Archive completed work and improve reusable skills.

Actions:
- archive: store a finished task (type, goal, outcome, steps, lessons). Do this after a long task.
- distill: show the shortest successful path for the latest task (or task_id).
- skill: distil the latest task into a skill and let the flywheel decide create / update / upgrade / skip.
- use: record that a skill was reused (name + success) so the report can rank it.
- report: current flywheel state, skill ranking and Mermaid graph.
- export: write report.md / report.json / graph.mmd under .mocode/flywheel/exports/.
- context: compact digest (tasks, skills, last path) for pulling history into context cheaply.

Promotion rules (applied by the engine, not the model): a skill is created when no similar
one exists, upgraded only when the new path is shorter, updated when new steps/lessons are
merged, and skipped when the draft adds nothing. Record failures honestly in steps.
