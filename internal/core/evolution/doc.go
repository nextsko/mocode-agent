// Package evolution is the self-evolution subsystem for mocode.
//
// The agent becomes more useful over time by automatically:
//
//   - Reviewing completed turns and writing durable user preferences /
//     project rules to long-term memory (Reviewer; inspired by
//     hermes-agent's background review).
//   - Consolidating episodic facts into long-term facts during idle time
//     (Dreamer; inspired by MiMo-Code /dream and grok-build /flush).
//   - Auto-tuning recall parameters with rollback guardrails (Evolution
//     cron; inspired by goclaw's evolution_cron).
//
// Design principles (learned from MiMo-Code / grok-build / goclaw /
// hermes-agent upstream survey, see
// docs/plans/self-evolution/00-upstream-survey.md):
//
//  1. Reuse the existing memory.Service — no new persistence.
//  2. Background jobs never block the main conversation (fail-open).
//  3. Dedup before write (drift guard, hermes-agent pattern).
//  4. Auto-tune is always reversible (goclaw guardrails pattern).
//  5. Pluggable callbacks (goclaw PipelineDeps injection pattern).
//  6. Stats in return values, not separate metrics services (goclaw
//     PruneStats pattern).
package evolution