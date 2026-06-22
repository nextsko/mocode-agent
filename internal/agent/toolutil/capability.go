package toolutil

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
type Startable interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// As performs a type assertion on tool, walking the Unwrapper chain until a
// value of type T is found or the chain ends.
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
