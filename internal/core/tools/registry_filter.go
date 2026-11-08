package tools

import (
	"context"

	"charm.land/fantasy"

	"github.com/nextsko/mocode-agent/internal/core/agent/agenttooladapter"
	"github.com/nextsko/mocode-agent/internal/core/tools/contract"
	"github.com/nextsko/mocode-agent/internal/core/tools/internalx/filter"
)

// BuildFiltered is like Build but applies zero or more FilterFuncs to the
// combined tool list before returning. Filters operate on contract.Tool, so
// the framework-backed tools are adapted at this boundary and mapped back to
// the originals afterwards. Descriptor-aware predicates (ByCategory, ByKind)
// require the descriptor map produced by AllToolDescriptors(), which is why
// they live here rather than in the filter sub-package.
func (r *Registry) BuildFiltered(ctx context.Context, deps ToolDeps, fns ...filter.FilterFunc) []fantasy.AgentTool {
	built := r.Build(ctx, deps)
	if len(fns) == 0 {
		return built
	}
	ports := make([]contract.Tool, len(built))
	byName := make(map[string]fantasy.AgentTool, len(built))
	for i, tool := range built {
		ports[i] = agenttooladapter.WrapFantasy(tool)
		byName[tool.Info().Name] = tool
	}
	allowed := filter.Apply(ctx, ports, fns...)
	out := make([]fantasy.AgentTool, 0, len(allowed))
	seen := make(map[string]struct{}, len(allowed))
	for _, port := range allowed {
		name := port.Name()
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		if tool, ok := byName[name]; ok {
			out = append(out, tool)
		}
	}
	return out
}

// ByCategory returns a FilterFunc that passes only tools in the given category.
// Tools not found in AllToolDescriptors (e.g. dynamically injected) are passed
// through so that they are never accidentally dropped.
func ByCategory(cat ToolCategory) filter.FilterFunc {
	descs := descriptorMap()
	return func(_ context.Context, tool contract.Tool) bool {
		d, ok := descs[tool.Name()]
		if !ok {
			return true // unknown → pass through
		}
		return d.Category == cat
	}
}

// ExcludeCategories returns a FilterFunc that blocks tools in any of the listed
// categories.  Unknown tools are passed through.
func ExcludeCategories(cats ...ToolCategory) filter.FilterFunc {
	block := make(map[ToolCategory]struct{}, len(cats))
	for _, c := range cats {
		block[c] = struct{}{}
	}
	descs := descriptorMap()
	return func(_ context.Context, tool contract.Tool) bool {
		d, ok := descs[tool.Name()]
		if !ok {
			return true // unknown → pass through
		}
		_, blocked := block[d.Category]
		return !blocked
	}
}

// ByKind returns a FilterFunc that passes only tools of the given kind.
// Unknown tools are passed through.
func ByKind(kind ToolKind) filter.FilterFunc {
	descs := descriptorMap()
	return func(_ context.Context, tool contract.Tool) bool {
		d, ok := descs[tool.Name()]
		if !ok {
			return true // unknown → pass through
		}
		return d.Kind == kind
	}
}

// descriptorMap builds a name→ToolDescriptor map from all standard plugins.
// It is called once per filter constructor to avoid re-computing on every call.
func descriptorMap() map[string]ToolDescriptor {
	all := AllToolDescriptors()
	m := make(map[string]ToolDescriptor, len(all))
	for _, d := range all {
		m[d.Name] = d
	}
	return m
}
