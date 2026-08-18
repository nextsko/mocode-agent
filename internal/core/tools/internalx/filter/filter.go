// Package filter provides context-aware filtering primitives for agent tools.
// It is inspired by the FilterFunc / FilterToolSet pattern in trpc-agent-go/tool/filter.go.
//
// FilterFunc operates on fantasy.AgentTool only; descriptor-aware predicates
// (ByCategory, ByKind) are defined in the parent tools package (registry_filter.go)
// to avoid an import cycle.
//
// Usage:
//
//	// Keep only bash and view tools:
//	out := filter.Apply(ctx, allTools, filter.IncludeNames("bash", "view"))
//
//	// Exclude specific tools:
//	out := filter.Apply(ctx, allTools, filter.ExcludeNames("fetch", "crawl", "download"))
//
//	// AND-chain multiple predicates:
//	out := filter.Apply(ctx, allTools,
//	    filter.Chain(filter.ExcludeNames("job_kill"), myCustomFilter))
package filter

import (
	"context"

	"charm.land/fantasy"
)

// FilterFunc decides whether a given tool should be included.
// ctx is the request-scoped context; implementations may read values from it
// to make dynamic decisions (e.g. agent role, session flags).
type FilterFunc func(ctx context.Context, tool fantasy.AgentTool) bool

// Apply returns only the tools for which all FilterFuncs return true.
// An empty fns slice is a no-op (returns the original slice unchanged).
func Apply(ctx context.Context, tools []fantasy.AgentTool, fns ...FilterFunc) []fantasy.AgentTool {
	if len(fns) == 0 {
		return tools
	}
	out := make([]fantasy.AgentTool, 0, len(tools))
	for _, t := range tools {
		if accepts(ctx, t, fns) {
			out = append(out, t)
		}
	}
	return out
}

// Chain combines multiple FilterFuncs into one with AND semantics: all must pass.
func Chain(fns ...FilterFunc) FilterFunc {
	return func(ctx context.Context, tool fantasy.AgentTool) bool {
		return accepts(ctx, tool, fns)
	}
}

// IncludeNames returns a FilterFunc that passes only tools whose name is in names.
func IncludeNames(names ...string) FilterFunc {
	allow := make(map[string]struct{}, len(names))
	for _, n := range names {
		allow[n] = struct{}{}
	}
	return func(_ context.Context, tool fantasy.AgentTool) bool {
		_, ok := allow[tool.Info().Name]
		return ok
	}
}

// ExcludeNames returns a FilterFunc that blocks any tool whose name is in names.
func ExcludeNames(names ...string) FilterFunc {
	block := make(map[string]struct{}, len(names))
	for _, n := range names {
		block[n] = struct{}{}
	}
	return func(_ context.Context, tool fantasy.AgentTool) bool {
		_, blocked := block[tool.Info().Name]
		return !blocked
	}
}

// accepts is a shared helper: returns true when all fns pass for the tool.
func accepts(ctx context.Context, tool fantasy.AgentTool, fns []FilterFunc) bool {
	for _, fn := range fns {
		if !fn(ctx, tool) {
			return false
		}
	}
	return true
}
