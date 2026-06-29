// Package filter provides framework-agnostic predicates for selecting
// tools out of a tools.Tool slice. The predicates operate on the
// toolkit's own Tool / ToolContext / Capable / Capability contract and
// do not depend on the LLM runtime, the agent package, or the config
// package. See Design Doc §6 and §7 for context.
package filter

import "github.com/package-register/mocode/tools"

// FilterFunc decides whether a tool passes the filter.
// Return true to keep the tool, false to drop it.
type FilterFunc func(tctx tools.ToolContext, t tools.Tool) bool

// Apply runs every FilterFunc against every tool and returns the
// surviving tools in their original order. A tool passes only if every
// filter returns true for it.
//
// The returned slice is a fresh allocation; the input slice is never
// mutated. When fns is empty, Apply returns a copy of the input so the
// order-preservation contract is uniform.
func Apply(tctx tools.ToolContext, all []tools.Tool, fns ...FilterFunc) []tools.Tool {
	out := make([]tools.Tool, 0, len(all))
	for _, tool := range all {
		pass := true
		for _, fn := range fns {
			if !fn(tctx, tool) {
				pass = false
				break
			}
		}
		if pass {
			out = append(out, tool)
		}
	}
	return out
}

// Chain combines multiple FilterFuncs into one. The result returns
// true only if every input FilterFunc returns true.
//
// Chain with no arguments returns a FilterFunc that always returns
// true, which is convenient when callers build filter lists at
// runtime and may end up with zero predicates.
func Chain(fns ...FilterFunc) FilterFunc {
	return func(tctx tools.ToolContext, t tools.Tool) bool {
		for _, fn := range fns {
			if !fn(tctx, t) {
				return false
			}
		}
		return true
	}
}

// IncludeNames keeps only tools whose Name() is in names.
func IncludeNames(names ...string) FilterFunc {
	set := make(map[string]struct{}, len(names))
	for _, n := range names {
		set[n] = struct{}{}
	}
	return func(_ tools.ToolContext, t tools.Tool) bool {
		_, ok := set[t.Name()]
		return ok
	}
}

// ExcludeNames drops tools whose Name() is in names.
func ExcludeNames(names ...string) FilterFunc {
	set := make(map[string]struct{}, len(names))
	for _, n := range names {
		set[n] = struct{}{}
	}
	return func(_ tools.ToolContext, t tools.Tool) bool {
		_, drop := set[t.Name()]
		return !drop
	}
}

// IncludeCapabilities keeps only tools that implement Capable and
// advertise at least one of the given capabilities.
//
// Tools that do not implement the Capable interface are dropped: the
// caller asked for a capability, and a tool that does not declare any
// cannot satisfy the request.
func IncludeCapabilities(caps ...tools.Capability) FilterFunc {
	wanted := make(map[tools.Capability]struct{}, len(caps))
	for _, c := range caps {
		wanted[c] = struct{}{}
	}
	return func(_ tools.ToolContext, t tools.Tool) bool {
		c, ok := t.(tools.Capable)
		if !ok {
			return false
		}
		for _, capability := range c.Capabilities() {
			if _, hit := wanted[capability]; hit {
				return true
			}
		}
		return false
	}
}

// BySource keeps only tools whose source meta equals source.
// NOTE: in Task 1.4 source meta is empty (filled in Task 2.1+). This
// function is therefore effectively a no-op for now; it is implemented
// so callers can compose it, and the meta-fill task will make it
// functional without changing this signature.
func BySource(source string) FilterFunc {
	_ = source
	return func(_ tools.ToolContext, _ tools.Tool) bool {
		return true
	}
}
