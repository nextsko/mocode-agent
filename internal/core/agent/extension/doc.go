// Package extension provides a composable lifecycle hook system for agents.
//
// It adapts trpc-agent-go's PluginManager/Callbacks pattern to mocode: an
// Extension registers on_xxx callbacks (before run, after run, before/after
// tool call, on message, on error) that the coordinator fires at well-defined
// points. Extensions are cross-cutting (logging, policy, observability,
// evolution capture) without per-agent configuration.
//
// The Manager owns a registry of named extensions and dispatches each
// callback. Callbacks are best-effort: a panicking or erroring callback is
// recovered and logged, never aborting the agent run, so a misbehaving
// extension cannot break the core loop. This matches mocode's existing
// "tools are self-documenting, hooks are shell commands" philosophy.
package extension
