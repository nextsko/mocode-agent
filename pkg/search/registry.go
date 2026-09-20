package search

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"sync"
)

// ProviderMeta describes a provider registration: display metadata plus the
// factory used to construct the singleton instance on first Load.
type ProviderMeta struct {
	// ID is the stable provider identifier.
	ID string

	// Label is the human-readable display name.
	Label string

	// NewFunc constructs a fresh provider instance. It runs at most once
	// per registry per ID.
	NewFunc func() ProviderV2

	// Priority orders the automatic chain; lower values run first.
	Priority int
}

// Registry holds provider metadata, an exclusion blacklist, and lazily
// materialized provider singletons.
type Registry struct {
	mu        sync.RWMutex
	metas     map[string]ProviderMeta
	sequence  []string // registration order, used as the tie-breaker
	excluded  map[string]struct{}
	instances sync.Map // provider ID -> ProviderV2
}

// NewRegistry creates a registry with an optional exclusion blacklist.
func NewRegistry(excluded ...string) *Registry {
	blacklist := make(map[string]struct{}, len(excluded))
	for _, id := range excluded {
		blacklist[id] = struct{}{}
	}
	return &Registry{
		metas:    make(map[string]ProviderMeta),
		excluded: blacklist,
	}
}

// Register adds provider metadata. It rejects empty IDs, nil factories, and
// duplicate registrations so init-time wiring fails fast.
func (r *Registry) Register(meta ProviderMeta) error {
	if meta.ID == "" {
		return errors.New("search: register provider: empty ID")
	}
	if meta.NewFunc == nil {
		return fmt.Errorf("search: register provider %q: nil NewFunc", meta.ID)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.metas[meta.ID]; dup {
		return fmt.Errorf("search: provider %q already registered", meta.ID)
	}
	r.metas[meta.ID] = meta
	r.sequence = append(r.sequence, meta.ID)
	return nil
}

// Load returns the singleton instance for id, constructing it on first use.
// Construction is guarded so NewFunc runs exactly once per ID even under
// concurrent calls; the instance is then retained in a sync.Map.
func (r *Registry) Load(id string) (ProviderV2, error) {
	if v, ok := r.instances.Load(id); ok {
		return v.(ProviderV2), nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if v, ok := r.instances.Load(id); ok {
		return v.(ProviderV2), nil
	}
	meta, ok := r.metas[id]
	if !ok {
		return nil, fmt.Errorf("search: unknown provider %q", id)
	}
	instance := meta.NewFunc()
	if instance == nil {
		return nil, fmt.Errorf("search: provider %q factory returned nil", id)
	}
	r.instances.Store(id, instance)
	return instance, nil
}

// Order returns candidate IDs sorted by ascending priority (registration
// order breaks ties), with blacklisted IDs removed.
func (r *Registry) Order() []string {
	type entry struct {
		id       string
		priority int
		seq      int
	}

	r.mu.RLock()
	entries := make([]entry, 0, len(r.sequence))
	for seq, id := range r.sequence {
		if _, skip := r.excluded[id]; skip {
			continue
		}
		entries = append(entries, entry{id: id, priority: r.metas[id].Priority, seq: seq})
	}
	r.mu.RUnlock()

	slices.SortFunc(entries, func(a, b entry) int {
		if c := cmp.Compare(a.priority, b.priority); c != 0 {
			return c
		}
		return cmp.Compare(a.seq, b.seq)
	})

	ordered := make([]string, len(entries))
	for i, e := range entries {
		ordered[i] = e.id
	}
	return ordered
}

// Excluded reports whether id is on the registry blacklist.
func (r *Registry) Excluded(id string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, skip := r.excluded[id]
	return skip
}
