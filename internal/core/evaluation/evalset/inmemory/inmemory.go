// Package inmemory provides an in-memory evalset.Manager for tests and local runs.
package inmemory

import (
	"context"
	"sync"

	"github.com/nextsko/mocode-agent/internal/core/evaluation/evalset"
)

// Manager is a concurrency-safe, in-memory evalset.Manager.
type Manager struct {
	mu   sync.RWMutex
	sets map[string]*evalset.EvalSet
}

// New returns an empty in-memory Manager.
func New() *Manager {
	return &Manager{sets: make(map[string]*evalset.EvalSet)}
}

// Put stores a set, replacing any existing set with the same ID.
func (m *Manager) Put(es *evalset.EvalSet) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sets[es.ID] = es
}

func (m *Manager) Get(_ context.Context, id string) (*evalset.EvalSet, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	es, ok := m.sets[id]
	if !ok {
		return nil, evalset.ErrNotFound
	}
	return es, nil
}

func (m *Manager) List(_ context.Context) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ids := make([]string, 0, len(m.sets))
	for id := range m.sets {
		ids = append(ids, id)
	}
	return ids, nil
}

func (m *Manager) AddCase(_ context.Context, id string, c *evalset.EvalCase) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	es, ok := m.sets[id]
	if !ok {
		return evalset.ErrNotFound
	}
	es.Cases = append(es.Cases, c)
	return nil
}

func (m *Manager) Close() error { return nil }
