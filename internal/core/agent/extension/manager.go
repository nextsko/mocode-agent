package extension

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

// Manager owns the set of active extensions and dispatches lifecycle events.
// It is safe for concurrent use. A zero-value Manager is ready to use and is
// a no-op until extensions are registered, so the coordinator can always hold
// one without nil checks.
type Manager struct {
	mu         sync.RWMutex
	// order preserves registration order so dispatch is deterministic.
	order []string
	// extensions maps name -> extension for lookup.
	extensions map[string]Extension
	// enabled tracks whether an extension name is active. Disabled extensions
	// are skipped on dispatch but stay registered, so the evo mode can toggle
	// observability without re-registering.
	enabled map[string]bool
}

// NewManager returns an empty Manager.
func NewManager() *Manager {
	return &Manager{
		extensions: make(map[string]Extension),
		enabled:    make(map[string]bool),
	}
}

// Register adds an extension, enabling it immediately. Re-registering an
// existing name replaces the prior extension and inherits its enabled state.
// Returns the manager for chaining.
func (m *Manager) Register(ext Extension) *Manager {
	m.mu.Lock()
	defer m.mu.Unlock()
	name := ext.Name()
	_, existed := m.extensions[name]
	m.extensions[name] = ext
	if !existed {
		m.order = append(m.order, name)
		m.enabled[name] = true
	}
	return m
}

// Unregister removes an extension by name. Missing names are a no-op.
func (m *Manager) Unregister(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.extensions, name)
	delete(m.enabled, name)
	for i, n := range m.order {
		if n == name {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
}

// Enable makes a registered extension active again.
func (m *Manager) Enable(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.extensions[name]; ok {
		m.enabled[name] = true
	}
}

// Disable suspends a registered extension without removing it.
func (m *Manager) Disable(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enabled[name] = false
}

// Has reports whether a named extension is registered (regardless of state).
func (m *Manager) Has(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.extensions[name]
	return ok
}

// Names returns the registered extension names in insertion-stable order.
func (m *Manager) Names() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]string(nil), m.order...)
}

// Dispatch fires an event to every enabled extension, in registration order.
// Each callback runs under a recover guard: a panic or error is logged and
// swallowed so a faulty extension never breaks the agent loop. The first
// non-nil Decision short-circuits dispatch and is returned to the caller
// (coordinator), which honors SkipTool/AbortRun. Returns nil when no
// extension produced a decision.
func (m *Manager) Dispatch(ctx context.Context, ev Event, ec *Context) *Decision {
	m.mu.RLock()
	// Build the active set under the read lock, in registration order; run
	// callbacks outside it so a callback that registers/unregisters cannot
	// deadlock.
	active := make([]Extension, 0, len(m.order))
	for _, name := range m.order {
		if m.enabled[name] {
			active = append(active, m.extensions[name])
		}
	}
	m.mu.RUnlock()

	for _, ext := range active {
		for _, cb := range ext.Callbacks() {
			dec, err := m.runCallback(ctx, ext.Name(), ev, ec, cb)
			if err != nil {
				continue // logged inside runCallback; never abort the loop
			}
			if dec != nil {
				return dec
			}
		}
	}
	return nil
}

// runCallback invokes one callback with panic recovery. A recovered panic is
// logged with the extension name and the event, then returned as an error so
// Dispatch skips the rest of that callback list without affecting others.
func (m *Manager) runCallback(
	ctx context.Context,
	extName string,
	ev Event,
	ec *Context,
	cb Callback,
) (dec *Decision, err error) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("Extension callback panicked",
				"extension", extName,
				"event", string(ev),
				"panic", fmt.Sprint(r),
			)
			err = fmt.Errorf("extension %s panicked on %s: %v", extName, ev, r)
			dec = nil
		}
	}()
	dec, err = cb(ctx, ev, ec)
	if err != nil {
		slog.Warn("Extension callback returned error",
			"extension", extName,
			"event", string(ev),
			"error", err,
		)
	}
	return dec, err
}

// Close releases every extension's resources, in registration order.
func (m *Manager) Close(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var firstErr error
	for _, name := range m.order {
		ext := m.extensions[name]
		if err := ext.Close(ctx); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("close extension %s: %w", name, err)
		}
	}
	m.extensions = make(map[string]Extension)
	m.enabled = make(map[string]bool)
	m.order = nil
	return firstErr
}

// FuncExtension is a convenience Extension built from a name and a single
// catch-all callback. Useful for small, inline extensions where defining a
// full type is overkill.
type FuncExtension struct {
	ExtensionName string
	Fn            Callback
	CloseFn       func(context.Context) error
}

// Name implements Extension.
func (f *FuncExtension) Name() string { return f.ExtensionName }

// Callbacks implements Extension by wrapping Fn for every event.
func (f *FuncExtension) Callbacks() []Callback {
	if f.Fn == nil {
		return nil
	}
	return []Callback{f.Fn}
}

// OnEvent implements Extension by delegating to Fn with a nil decision.
func (f *FuncExtension) OnEvent(ctx context.Context, ev Event, ec *Context) {
	if f.Fn != nil {
		_, _ = f.Fn(ctx, ev, ec)
	}
}

// Close implements Extension, defaulting to a no-op.
func (f *FuncExtension) Close(ctx context.Context) error {
	if f.CloseFn != nil {
		return f.CloseFn(ctx)
	}
	return nil
}
