// Package capability provides generic capability-discovery helpers for the
// fantasy.AgentTool decorator chain.
//
// Inspired by docker-agent/pkg/tools/startable.go and capabilities.go.
//
// The mocode tool stack looks like:
//
//	hookedTool → wrappedTool (callback) → retryTool → inner tool
//
// As[T] lets callers ask "does anything in this chain implement T?" without
// knowing how deep the stack is.  Each decorator must implement Unwrapper to
// participate in the walk.
//
// Usage:
//
//	if instr, ok := capability.As[capability.Instructable](tool); ok {
//	    systemPrompt += instr.Instructions()
//	}
package capability

import (
	"context"

	"charm.land/fantasy"
)

// Unwrapper is implemented by tool decorators that wrap another AgentTool.
// As[T] uses this interface to walk the decorator chain.
type Unwrapper interface {
	Unwrap() fantasy.AgentTool
}

// Instructable is implemented by tools that want to inject additional text
// into the system prompt at runtime.
type Instructable interface {
	Instructions() string
}

// Startable is implemented by tools (or tool plugins) that require
// initialization before use and explicit teardown after.
// Inspired by docker-agent/pkg/tools/capabilities.go.
type Startable interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// As performs a type assertion on tool, walking the Unwrapper chain until a
// value of type T is found or the chain ends.
//
// The outermost tool is checked first; if it does not satisfy T, As calls
// Unwrap (if available) and repeats.  A nil tool returns (zero, false).
func As[T any](tool fantasy.AgentTool) (T, bool) {
	for tool != nil {
		if result, ok := any(tool).(T); ok {
			return result, true
		}
		if u, ok := tool.(Unwrapper); ok {
			tool = u.Unwrap()
		} else {
			break
		}
	}
	var zero T
	return zero, false
}

// GetInstructions returns the Instructions() string if tool (or any tool in
// its decorator chain) implements Instructable.  Returns "" otherwise.
func GetInstructions(tool fantasy.AgentTool) string {
	if i, ok := As[Instructable](tool); ok {
		return i.Instructions()
	}
	return ""
}
