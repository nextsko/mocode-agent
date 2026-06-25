// Package inmemory provides an in-memory metric.Manager for tests and local runs.
package inmemory

import (
	"context"
	"sync"

	"github.com/package-register/mocode/internal/evaluation/metric"
)

// Manager is a concurrency-safe, in-memory metric.Manager.
type Manager struct {
	mu       sync.RWMutex
	metrics  map[string]*metric.EvalMetric
}

// New returns an empty in-memory Manager.
func New() *Manager { return &Manager{metrics: map[string]*metric.EvalMetric{}} }

// Put stores a metric, replacing any existing metric with the same name.
func (m *Manager) Put(em *metric.EvalMetric) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.metrics[em.Name] = em
}

func (m *Manager) List(context.Context) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.metrics))
	for name := range m.metrics {
		names = append(names, name)
	}
	return names, nil
}

func (m *Manager) Get(_ context.Context, name string) (*metric.EvalMetric, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	em, ok := m.metrics[name]
	if !ok {
		return nil, metric.ErrNotFound
	}
	return em, nil
}

func (m *Manager) Add(_ context.Context, em *metric.EvalMetric) error {
	m.Put(em)
	return nil
}

func (m *Manager) Close() error { return nil }
