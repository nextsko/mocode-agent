package authhandler

import (
	"context"
	"fmt"
	"io"
	"sort"

	"github.com/nextsko/mocode-agent/internal/core/config"
	"github.com/nextsko/mocode-agent/internal/transport/workspace"
)

// Env carries shared dependencies for auth handlers.
type Env struct {
	Store     *config.ConfigStore
	Workspace workspace.Workspace
	Stdout    io.Writer
	Stderr    io.Writer
}

// Handler authenticates mocode with one external service.
type Handler interface {
	ID() string
	Description() string
	Login(ctx context.Context, env Env) error
}

var handlers = map[string]Handler{}

// Register adds an auth handler. It panics on duplicate IDs because handlers are
// process-global command wiring and duplicates indicate a programming error.
func Register(h Handler) {
	id := h.ID()
	if id == "" {
		panic("auth handler ID is empty")
	}
	if _, ok := handlers[id]; ok {
		panic(fmt.Sprintf("auth handler %q already registered", id))
	}
	handlers[id] = h
}

// Get returns a registered auth handler by ID.
func Get(id string) (Handler, bool) {
	h, ok := handlers[id]
	return h, ok
}

// IDs returns all registered handler IDs sorted for stable help and completion.
func IDs() []string {
	ids := make([]string, 0, len(handlers))
	for id := range handlers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
