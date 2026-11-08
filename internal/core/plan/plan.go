// Package plan stores a per-session execution plan. It is deliberately small
// and observable: the model sets it through the `plan` tool, the TUI and ACP
// both subscribe to it. A shared source prevents the agent from maintaining a
// private checklist that clients cannot see.
package plan

import (
	"sync"
)

// Status is the state of one plan entry.
type Status string

const (
	StatusPending    Status = "pending"
	StatusInProgress Status = "in_progress"
	StatusCompleted  Status = "completed"
)

// Item is one step of a session plan.
type Item struct {
	ID       string `json:"id"`
	Content  string `json:"content"`
	Status   Status `json:"status"`
	Priority string `json:"priority,omitempty"` // high | medium | low
}

// Store keeps plans per session and fans out changes to subscribers.
type Store struct {
	mu    sync.Mutex
	plans map[string][]Item
	subs  map[string]map[chan []Item]struct{}
}

// New creates an empty store.
func New() *Store {
	return &Store{plans: map[string][]Item{}, subs: map[string]map[chan []Item]struct{}{}}
}

var defaultStore = New()

// Default returns the process-wide store used by the tool and transports.
func Default() *Store { return defaultStore }

// Set replaces the whole plan for a session.
func (s *Store) Set(sessionID string, items []Item) {
	s.mu.Lock()
	cp := append([]Item{}, items...)
	s.plans[sessionID] = cp
	s.notifyLocked(sessionID, cp)
	s.mu.Unlock()
}

// Get returns a copy of the current plan.
func (s *Store) Get(sessionID string) []Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Item{}, s.plans[sessionID]...)
}

// Add appends one item and returns it.
func (s *Store) Add(sessionID string, item Item) Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	if item.ID == "" {
		item.ID = "step-" + itoa(len(s.plans[sessionID])+1)
	}
	if item.Status == "" {
		item.Status = StatusPending
	}
	s.plans[sessionID] = append(s.plans[sessionID], item)
	s.notifyLocked(sessionID, append([]Item{}, s.plans[sessionID]...))
	return item
}

// Update changes one item's content/status/priority. It reports whether the id
// existed.
func (s *Store) Update(sessionID, id string, mutate func(*Item)) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.plans[sessionID] {
		if s.plans[sessionID][i].ID != id {
			continue
		}
		mutate(&s.plans[sessionID][i])
		s.notifyLocked(sessionID, append([]Item{}, s.plans[sessionID]...))
		return true
	}
	return false
}

// Remove deletes one item.
func (s *Store) Remove(sessionID, id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, item := range s.plans[sessionID] {
		if item.ID != id {
			continue
		}
		s.plans[sessionID] = append(s.plans[sessionID][:i], s.plans[sessionID][i+1:]...)
		s.notifyLocked(sessionID, append([]Item{}, s.plans[sessionID]...))
		return true
	}
	return false
}

// Clear removes the plan.
func (s *Store) Clear(sessionID string) {
	s.mu.Lock()
	delete(s.plans, sessionID)
	s.notifyLocked(sessionID, nil)
	s.mu.Unlock()
}

// Subscribe returns plan updates until the returned cancel func is called.
// The current plan is delivered immediately so a late subscriber is in sync.
func (s *Store) Subscribe(sessionID string) (<-chan []Item, func()) {
	ch := make(chan []Item, 8)
	s.mu.Lock()
	if s.subs[sessionID] == nil {
		s.subs[sessionID] = map[chan []Item]struct{}{}
	}
	s.subs[sessionID][ch] = struct{}{}
	current := append([]Item{}, s.plans[sessionID]...)
	s.mu.Unlock()

	select {
	case ch <- current:
	default:
	}
	cancel := func() {
		s.mu.Lock()
		if subs := s.subs[sessionID]; subs != nil {
			delete(subs, ch)
			if len(subs) == 0 {
				delete(s.subs, sessionID)
			}
		}
		s.mu.Unlock()
		close(ch)
	}
	return ch, cancel
}

func (s *Store) notifyLocked(sessionID string, items []Item) {
	for ch := range s.subs[sessionID] {
		select {
		case ch <- append([]Item{}, items...):
		default:
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
