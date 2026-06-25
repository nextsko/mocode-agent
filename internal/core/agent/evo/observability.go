package evo

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/package-register/mocode/internal/core/agent/extension"
)

// ObservabilityExtension is the extension registered with the agent
// coordinator while a /evo session is active. It observes every run and folds
// successful turns into the shared evo State, which is what the self-evolving
// loop reconstructs the "optimal theory" from. On AfterRun it records the
// refined prompt; on OnError it logs the failure so it becomes evolution data.
//
// It is safe for concurrent use: the coordinator may dispatch from multiple
// goroutines, and the State is guarded by a mutex.
type ObservabilityExtension struct {
	mu    sync.Mutex
	state *State
}

// NewObservabilityExtension wires the extension to a shared evo State. The
// state is owned by the TUI model; the extension holds a pointer so iterations
// accumulate across runs into the same state that /evo exit will fix.
func NewObservabilityExtension(state *State) *ObservabilityExtension {
	return &ObservabilityExtension{state: state}
}

// Name implements extension.Extension.
func (o *ObservabilityExtension) Name() string { return "evo.observability" }

// Callbacks returns the on_xxx handlers this extension registers.
func (o *ObservabilityExtension) Callbacks() []extension.Callback {
	return []extension.Callback{o.onEvent}
}

// onEvent is the catch-all dispatcher. It only acts on AfterRun and OnError;
// every other event is a no-op (pure observation).
func (o *ObservabilityExtension) onEvent(ctx context.Context, ev extension.Event, ec *extension.Context) (*extension.Decision, error) {
	switch ev {
	case extension.EventAfterRun:
		if ec == nil || strings.TrimSpace(ec.Prompt) == "" {
			return nil, nil
		}
		o.mu.Lock()
		defer o.mu.Unlock()
		// Fold the successful turn: the user's prompt becomes part of the
		// reconstructed optimal theory. A real implementation would distill
		// the prompt + response into a refined principle; the baseline keeps
		// the latest successful prompt as the working theory.
		// Capture the tools exercised during the run as emergent skills:
		// each distinct tool name is a capability the agent demonstrated.
		o.state.RecordIteration(ec.Prompt, ec.Response, ec.ToolNames)
		slog.Debug("evo: recorded iteration",
			"agent", ec.AgentName, "iterations", o.state.Iterations)
	case extension.EventOnError:
		if ec != nil && ec.Err != nil {
			slog.Warn("evo: observed run error (evolution data)",
				"agent", ec.AgentName, "error", ec.Err)
		}
	}
	return nil, nil
}

// OnEvent implements extension.Extension for events without a structured callback.
func (o *ObservabilityExtension) OnEvent(ctx context.Context, ev extension.Event, ec *extension.Context) {
	_, _ = o.onEvent(ctx, ev, ec)
}

// Close implements extension.Extension.
func (o *ObservabilityExtension) Close(ctx context.Context) error { return nil }

// Compile-time assertion that ObservabilityExtension satisfies Extension.
var _ extension.Extension = (*ObservabilityExtension)(nil)
