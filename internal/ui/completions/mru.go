package completions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/nextsko/mocode-agent/internal/util/infra"
)

// mruSize caps the remembered slash commands (most-recent-first).
const mruSize = 32

// slashMRU persists recently used slash commands so the bare "/" menu can
// surface them first (grok-build's slash/mru.rs idea). File location:
// <data dir>/slash-mru.json.
type slashMRU struct {
	mu   sync.Mutex
	rank map[string]time.Time
}

func mruPath() string {
	return filepath.Join(infra.DataDir(), "slash-mru.json")
}

func loadMRU() *slashMRU {
	m := &slashMRU{rank: map[string]time.Time{}}
	data, err := os.ReadFile(mruPath())
	if err != nil {
		return m
	}
	// map[string]time.Time round-trips through JSON.
	_ = json.Unmarshal(data, &m.rank)
	return m
}

func (m *slashMRU) record(cmd string) {
	if cmd == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rank[cmd] = time.Now()
	if len(m.rank) > mruSize {
		// Evict the oldest entry.
		var oldestKey string
		var oldest time.Time
		first := true
		for k, v := range m.rank {
			if first || v.Before(oldest) {
				oldestKey, oldest, first = k, v, false
			}
		}
		delete(m.rank, oldestKey)
	}
	data, err := json.Marshal(m.rank)
	if err != nil {
		return
	}
	_ = os.WriteFile(mruPath(), data, 0o600)
}

// rankOf returns the recency rank (larger = more recent; 0 = never used).
func (m *slashMRU) rankOf(cmd string) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	mostRecent := int64(0)
	for _, v := range m.rank {
		if ts := v.Unix(); ts > mostRecent {
			mostRecent = ts
		}
	}
	ts := m.rank[cmd].Unix()
	if ts == 0 {
		return 0
	}
	// More recent ⇒ higher rank.
	return ts - mostRecent + int64(len(m.rank))
}
