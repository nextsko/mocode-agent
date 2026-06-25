package roundtable

import (
	"fmt"
)

// ToolGuard decides whether a tool may be invoked in a given phase.
type ToolGuard struct {
	AllowedInDiscussion map[string]bool
}

// NewToolGuard creates a guard that permits only read-only tools during discussion.
func NewToolGuard() *ToolGuard {
	return &ToolGuard{AllowedInDiscussion: DefaultReadOnlyTools}
}

// CanUse returns true if the tool is allowed in the given phase.
func (g *ToolGuard) CanUse(phase Phase, toolName string) bool {
	if phase == PhaseExecution || phase == PhaseDone {
		return true
	}
	return g.AllowedInDiscussion[toolName]
}

// Deny returns a formatted error for a disallowed tool.
func (g *ToolGuard) Deny(phase Phase, toolName string) error {
	return fmt.Errorf("tool %q is not allowed during phase %q; read-only tools only until execution", toolName, phase)
}
