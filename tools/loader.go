// Package tools: loader.go — package-level forwarding functions over
// the default Registry.
//
// Tool subpackages register themselves with the package-level Register /
// RegisterProvider helpers from their own init() functions. The agent
// runtime reads the resulting set via Default() (or the package-level
// Get / Names / All helpers).
//
// Each forwarding function resolves the caller's import path via
// runtime.Caller(1) and passes it to the registry directly. Doing the
// caller lookup here (rather than inside Registry.Register) keeps the
// loader wrapper itself out of the recorded source package.
package tools

// defaultRegistry is the package-level default registry. Use Default()
// to access it; NewRegistry() for an isolated instance.
var defaultRegistry = NewRegistry()

// Default returns the package-level default registry.
func Default() *Registry { return defaultRegistry }

// Register registers tool under name on the default registry. The
// caller's import path (the init() in the tool's package) is recorded
// as the tool's source package.
func Register(name string, tool Tool) {
	defaultRegistry.mu.Lock()
	defer defaultRegistry.mu.Unlock()
	defaultRegistry.registerLocked(name, tool, callerPackage(1))
}

// RegisterProvider registers every tool from p on the default registry.
// All tools share the caller's import path as their source package.
func RegisterProvider(p ToolProvider) {
	defaultRegistry.mu.Lock()
	defer defaultRegistry.mu.Unlock()
	defaultRegistry.registerProviderLocked(p, callerPackage(1))
}

// Get returns the tool registered under name on the default registry.
func Get(name string) (Tool, bool) { return defaultRegistry.Get(name) }

// Names returns the names of tools on the default registry, sorted.
func Names() []string { return defaultRegistry.Names() }

// All returns a snapshot of all tools on the default registry.
func All() []Tool { return defaultRegistry.All() }
