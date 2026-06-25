package evalset

import "context"

// EvalSet is a named, ordered collection of EvalCases.
// One EvalSet corresponds to one evaluation run.
type EvalSet struct {
	// ID uniquely identifies this evaluation set.
	ID string
	// Name is a human-readable label.
	Name string
	// Description explains what the set covers.
	Description string
	// Cases are the evaluation cases, evaluated in order.
	Cases []*EvalCase
}

// Manager stores and retrieves evaluation sets and their cases.
// Implementations may be in-memory (tests) or backed by the mocode SQLite store.
type Manager interface {
	// Get returns the EvalSet identified by id, including all its cases.
	Get(ctx context.Context, id string) (*EvalSet, error)
	// List returns the IDs of all known evaluation sets.
	List(ctx context.Context) ([]string, error)
	// AddCase appends a case to the set identified by id.
	AddCase(ctx context.Context, id string, c *EvalCase) error
	// Close releases resources held by the manager.
	Close() error
}

// ErrNotFound is returned by Manager.Get when no set exists for the given id.
var ErrNotFound = errNotFound{}

type errNotFound struct{}

func (errNotFound) Error() string { return "evalset: not found" }

func (errNotFound) Is(target error) bool {
	_, ok := target.(errNotFound)
	return ok
}
