package memory

import (
	"slices"
	"strings"
	"sync"

	"charm.land/fantasy"
)

// ToolOptions configures memory tool registration for any Service implementation.
type ToolOptions struct {
	MaxSearchResults int
	EnabledTools     map[string]bool
}

// DefaultToolOptions returns default tool options (clear disabled).
func DefaultToolOptions() ToolOptions {
	return ToolOptions{
		MaxSearchResults: DefaultMaxSearchResults,
		EnabledTools: map[string]bool{
			AddToolName:    true,
			UpdateToolName: true,
			DeleteToolName: true,
			SearchToolName: true,
			LoadToolName:   true,
		},
	}
}

// toolProvider builds fantasy tools backed by any Service.
type toolProvider struct {
	svc              Service
	maxSearchResults int
	enabledTools     map[string]bool
	cachedTools      map[string]fantasy.AgentTool
	toolsMu          sync.RWMutex
}

// ToolsFor returns agent tools for the given memory service.
func ToolsFor(svc Service, opts ToolOptions) []fantasy.AgentTool {
	if opts.MaxSearchResults <= 0 {
		opts.MaxSearchResults = DefaultMaxSearchResults
	}
	if opts.EnabledTools == nil {
		opts.EnabledTools = DefaultToolOptions().EnabledTools
	}
	p := &toolProvider{
		svc:              svc,
		maxSearchResults: opts.MaxSearchResults,
		enabledTools:     opts.EnabledTools,
		cachedTools:      make(map[string]fantasy.AgentTool),
	}
	return p.Tools()
}

func (p *toolProvider) Tools() []fantasy.AgentTool {
	p.toolsMu.Lock()
	defer p.toolsMu.Unlock()

	names := []string{
		AddToolName, UpdateToolName, DeleteToolName,
		ClearToolName, SearchToolName, LoadToolName,
	}

	tools := make([]fantasy.AgentTool, 0, len(names))
	for _, name := range names {
		if !p.enabledTools[name] {
			continue
		}
		if _, ok := p.cachedTools[name]; !ok {
			p.cachedTools[name] = p.createTool(name)
		}
		if t := p.cachedTools[name]; t != nil {
			tools = append(tools, t)
		}
	}

	slices.SortFunc(tools, func(a, b fantasy.AgentTool) int {
		return strings.Compare(a.Info().Name, b.Info().Name)
	})
	return tools
}

func (p *toolProvider) createTool(name string) fantasy.AgentTool {
	s := &service{maxSearchResults: p.maxSearchResults}
	return s.createTool(name)
}
