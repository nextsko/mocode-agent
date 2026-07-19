package config

import "fmt"

// Scope determines which config file is targeted for read/write operations.
type Scope int

const (
	// ScopeGlobal targets the global data config (~/.local/share/mocode/mocode.json).
	ScopeGlobal Scope = iota
)

// String returns a human-readable label for the scope.
func (s Scope) String() string {
	switch s {
	case ScopeGlobal:
		return "global"
	default:
		return fmt.Sprintf("Scope(%d)", int(s))
	}
}
