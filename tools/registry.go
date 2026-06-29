// Package tools: registry.go — runtime core of the toolkit.
//
// This file is intentionally framework-agnostic: it imports nothing from
// charm.land/fantasy, charm.land/catwalk, internal/core/agent, or
// internal/core/config. The Registry is consumed by the agent runtime via
// its public Tool interface only; the agent does not see these types
// directly. See Design Doc §6 for the full rationale.
package tools

import (
	"log/slog"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// sourceMeta is metadata about a tool's registration source. Recorded
// for diagnostics when duplicate names trigger slog.Warn.
type sourceMeta struct {
	Source       string // "builtin" | "plugin" | "mcp" | "lsp" | ...
	Package      string // import path, e.g. "github.com/package-register/mocode/tools/builtin/bash"
	RegisteredAt time.Time
}

// Registry is a thread-safe name -> Tool map. The default singleton
// (Default()) is the typical entry point; NewRegistry() builds an
// isolated instance for tests or alternative tool sets.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
	meta  map[string]sourceMeta
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
		meta:  make(map[string]sourceMeta),
	}
}

// Register adds tool under name, overwriting any existing entry with
// the same name. On overwrite, logs a slog.Warn with the previous and
// new source package paths. Safe to call from init() and concurrently
// with Get/Names/All.
//
// The caller package is resolved from the immediate caller of Register.
// Callers that go through a forwarding wrapper (e.g. the package-level
// Register in loader.go) should resolve the package at the outer
// boundary and call registerLocked directly, so the wrapper itself
// never appears as the recorded source package.
func (r *Registry) Register(name string, tool Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.registerLocked(name, tool, callerPackage(1))
}

// RegisterProvider registers every tool returned by p.Tools() under
// the tool's own Name(). Equivalent to calling Register on each.
// On collision, the provider's later-registered tool wins.
//
// As with Register, the caller package is the immediate caller of
// RegisterProvider. Forwarding wrappers that wish to attribute the
// batch to the outer caller should call registerProviderLocked
// directly with a pre-resolved package.
func (r *Registry) RegisterProvider(p ToolProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.registerProviderLocked(p, callerPackage(1))
}

// registerProviderLocked registers every tool from p under its own
// Name(), attributing all entries to callerPkg. The caller must hold
// r.mu. Empty callerPkg is allowed and means "unknown".
func (r *Registry) registerProviderLocked(p ToolProvider, callerPkg string) {
	for _, tool := range p.Tools() {
		r.registerLocked(tool.Name(), tool, callerPkg)
	}
}

// registerLocked inserts a tool under name. The caller must hold r.mu.
// callerPkg is the import path of the package that triggered the
// registration; an empty string means "unknown".
func (r *Registry) registerLocked(name string, tool Tool, callerPkg string) {
	if prevMeta, exists := r.meta[name]; exists {
		slog.Warn(
			"tool registration replaced existing entry",
			"name", name,
			"prev_package", prevMeta.Package,
			"new_package", callerPkg,
		)
	}
	r.tools[name] = tool
	r.meta[name] = sourceMeta{
		// Source is left empty for now; Task 2.1 introduces a helper
		// init() that backfills it once tools are migrated.
		Source:       "",
		Package:      callerPkg,
		RegisteredAt: time.Now(),
	}
}

// Get returns the tool registered under name, or (nil, false) if absent.
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// Names returns all registered tool names in lexicographic order.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.tools))
	for name := range r.tools {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// All returns a snapshot of all registered tools. The order is unspecified.
func (r *Registry) All() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	return out
}

// callerPackage returns the import path of the function `skip` frames
// above callerPackage's own call site. Callers should pass skip=1 to
// mean "the function that called me". An empty string is returned when
// the stack cannot be walked (e.g. the skip is too deep).
func callerPackage(skip int) string {
	// runtime.Caller(0) names the function that contains the
	// runtime.Caller call, i.e. callerPackage itself. Add 1 so
	// callerPackage(0) means "callerPackage" and callerPackage(1)
	// means "caller of callerPackage".
	pc, _, _, ok := runtime.Caller(skip + 1)
	if !ok {
		return ""
	}
	fn := runtime.FuncForPC(pc)
	if fn == nil {
		return ""
	}
	return packageFromFuncName(fn.Name())
}

// packageFromFuncName extracts the import path from a fully-qualified
// runtime function name. The name has one of these shapes:
//
//	"github.com/foo/bar.Func"            — top-level function
//	"github.com/foo/bar.(*Type).Method"  — method
//	"github.com/foo/bar.init"            — package init
//	"github.com/foo/bar.init.0"          — anonymous func inside init
//	"github.com/foo/bar.init.func1"      — Go 1.21+ named sub-init
//
// In every case the package boundary is the first '.' after the last
// '/'. If neither is present, the whole name is returned.
func packageFromFuncName(name string) string {
	lastSlash := strings.LastIndex(name, "/")
	start := lastSlash + 1
	if lastSlash < 0 {
		start = 0
	}
	rest := name[start:]
	dot := strings.Index(rest, ".")
	if dot < 0 {
		return name
	}
	return name[:start+dot]
}
