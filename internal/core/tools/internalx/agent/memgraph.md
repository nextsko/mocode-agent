Structured, traceable memory: a graph of who the user is, who you are, what they prefer, what was decided, and why.

Actions:
- remember: upsert a node (kind = user/persona/preference/fact/goal/decision/task/skill/project/event/artifact) with props, confidence and evidence (source_quote/source_ref). Repeated observations merge instead of overwriting.
- relate: add an edge between two node ids (from/to/edge_kind: prefers, knows, decided, produced, derived_from, supersedes, contradicts, relates_to, part_of, evidenced_by).
- recall: search nodes by kind/query; with `id` returns the node's neighbourhood up to `depth`.
- explain: evidence chains explaining why a belief is held (walks to sources).
- persona: render the AI persona + user profile card (preferences, goals, unresolved conflicts).
- conflicts: list contradictory relationships so they can be resolved by superseding.
- forget: delete a node and its edges (user-requested privacy).
- export: write graph.json / graph.mmd / graph.html under .mocode/memory/exports/.
- graph: return the Mermaid graph.
- decay: down-weight stale beliefs (half_life_days).

Evidence is mandatory for durable facts: give a short quote and its ref (session id, message id, URL). Contradictions should be resolved with relate edge_kind=supersedes, not by deleting history.
