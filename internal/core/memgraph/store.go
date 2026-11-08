package memgraph

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// RootDir is where the graph and its event log live.
const RootDir = ".mocode/memory"

// Store is a concurrency-safe handle to one project's memory graph.
type Store struct {
	mu   sync.Mutex
	dir  string
	path string
	log  string
	g    Graph
}

// Open loads (or initialises) the graph under workingDir.
func Open(workingDir string) (*Store, error) {
	dir := filepath.Join(workingDir, RootDir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create memory dir: %w", err)
	}
	s := &Store{dir: dir, path: filepath.Join(dir, "graph.json"), log: filepath.Join(dir, "events.jsonl")}
	raw, err := os.ReadFile(s.path)
	switch {
	case err == nil:
		var g Graph
		if err := json.Unmarshal(raw, &g); err != nil {
			return nil, fmt.Errorf("parse memory graph: %w", err)
		}
		s.g = g
	case os.IsNotExist(err):
		s.g = Graph{Version: 1, Nodes: map[string]Node{}, Edges: map[string]Edge{}}
	default:
		return nil, fmt.Errorf("read memory graph: %w", err)
	}
	s.g.Nodes = nonNil(s.g.Nodes)
	s.g.Edges = nonNil(s.g.Edges)
	return s, nil
}

func nonNil[K comparable, V any](m map[K]V) map[K]V {
	if m == nil {
		return map[K]V{}
	}
	return m
}

// Graph returns a deep-ish copy for readers.
func (s *Store) Graph() Graph {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := Graph{Version: s.g.Version, UpdatedAt: s.g.UpdatedAt, Nodes: map[string]Node{}, Edges: map[string]Edge{}}
	for k, v := range s.g.Nodes {
		out.Nodes[k] = v
	}
	for k, v := range s.g.Edges {
		out.Edges[k] = v
	}
	return out
}

// UpsertNode merges a node: new props win, confidence is averaged, sources
// and tags accumulate. Merging (rather than replacing) is what lets repeated
// observations strengthen a belief instead of overwriting its history.
func (s *Store) UpsertNode(n Node) (Node, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	if n.ID == "" {
		n.ID = slug(n.Kind + ":" + n.Name)
	}
	if n.Confidence <= 0 {
		n.Confidence = 0.6
	}
	existing, ok := s.g.Nodes[n.ID]
	if ok {
		n.CreatedAt = existing.CreatedAt
		n.Props = mergeMaps(existing.Props, n.Props)
		n.Tags = mergeStrings(existing.Tags, n.Tags)
		n.Sources = mergeSources(existing.Sources, n.Sources)
		n.Confidence = (existing.Confidence + n.Confidence) / 2
		if existing.SupersededBy != "" {
			n.SupersededBy = existing.SupersededBy
		}
		if existing.ValidFrom != nil {
			n.ValidFrom = existing.ValidFrom
		}
	} else {
		n.CreatedAt = now
	}
	n.UpdatedAt = now
	s.g.Nodes[n.ID] = n
	s.g.UpdatedAt = now
	if err := s.persistLocked(Event{At: now, Op: "upsert_node", Node: &n}); err != nil {
		return Node{}, err
	}
	return n, nil
}

// UpsertEdge merges an edge keyed by from|kind|to.
func (s *Store) UpsertEdge(e Edge) (Edge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	if e.From == "" || e.To == "" || e.Kind == "" {
		return Edge{}, fmt.Errorf("edge needs from, to and kind")
	}
	if _, ok := s.g.Nodes[e.From]; !ok {
		return Edge{}, fmt.Errorf("edge source node %q not found", e.From)
	}
	if _, ok := s.g.Nodes[e.To]; !ok {
		return Edge{}, fmt.Errorf("edge target node %q not found", e.To)
	}
	if e.ID == "" {
		e.ID = slug(e.From + "|" + e.Kind + "|" + e.To)
	}
	if e.Weight <= 0 {
		e.Weight = 1
	}
	if e.Confidence <= 0 {
		e.Confidence = 0.6
	}
	if old, ok := s.g.Edges[e.ID]; ok {
		e.CreatedAt = old.CreatedAt
		e.Sources = mergeSources(old.Sources, e.Sources)
		e.Confidence = (old.Confidence + e.Confidence) / 2
		e.Weight = (old.Weight + e.Weight) / 2
	} else {
		e.CreatedAt = now
	}
	s.g.Edges[e.ID] = e
	s.g.UpdatedAt = now
	if err := s.persistLocked(Event{At: now, Op: "upsert_edge", Edge: &e}); err != nil {
		return Edge{}, err
	}
	return e, nil
}

// Forget removes a node and every edge touching it.
func (s *Store) Forget(id string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := 0
	if _, ok := s.g.Nodes[id]; ok {
		delete(s.g.Nodes, id)
		removed++
	}
	for k, e := range s.g.Edges {
		if e.From == id || e.To == id {
			delete(s.g.Edges, k)
			removed++
		}
	}
	if removed == 0 {
		return 0, fmt.Errorf("node %q not found", id)
	}
	now := time.Now().UTC()
	s.g.UpdatedAt = now
	return removed, s.persistLocked(Event{At: now, Op: "forget", ID: id})
}

// Supersede marks oldID as replaced by newID while keeping the old belief
// queryable (valid_to + superseded_by), so explanations can show evolution.
func (s *Store) Supersede(oldID, newID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.g.Nodes[oldID]
	if !ok {
		return fmt.Errorf("node %q not found", oldID)
	}
	if _, ok := s.g.Nodes[newID]; !ok {
		return fmt.Errorf("replacement %q not found", newID)
	}
	now := time.Now().UTC()
	old.SupersededBy = newID
	old.ValidTo = &now
	old.UpdatedAt = now
	s.g.Nodes[oldID] = old
	s.g.UpdatedAt = now
	if err := s.persistLocked(Event{At: now, Op: "supersede", ID: oldID}); err != nil {
		return err
	}
	s.mu.Unlock()
	_, err := s.UpsertEdge(Edge{From: newID, To: oldID, Kind: EdgeSupersedes, Confidence: 0.9})
	s.mu.Lock()
	return err
}

// Get returns one node.
func (s *Store) Get(id string) (Node, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, ok := s.g.Nodes[id]
	return n, ok
}

// Neighbors walks the graph outward up to depth hops.
func (s *Store) Neighbors(id string, depth int) ([]Node, []Edge) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if depth <= 0 {
		depth = 1
	}
	seen := map[string]bool{id: true}
	frontier := []string{id}
	var nodes []Node
	var edges []Edge
	edgeSeen := map[string]bool{}
	for d := 0; d < depth; d++ {
		var next []string
		for _, cur := range frontier {
			for _, e := range s.g.Edges {
				if e.From != cur && e.To != cur {
					continue
				}
				if !edgeSeen[e.ID] {
					edgeSeen[e.ID] = true
					edges = append(edges, e)
				}
				other := e.To
				if e.To == cur {
					other = e.From
				}
				if !seen[other] {
					seen[other] = true
					next = append(next, other)
					if n, ok := s.g.Nodes[other]; ok {
						nodes = append(nodes, n)
					}
				}
			}
		}
		frontier = next
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Kind < nodes[j].Kind })
	return nodes, edges
}

// Search filters nodes by kind and a case-insensitive name/prop substring.
func (s *Store) Search(kind, contains string) []Node {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Node
	q := strings.ToLower(contains)
	for _, n := range s.g.Nodes {
		if kind != "" && n.Kind != kind {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(n.Name+" "+fmt.Sprint(n.Props)), q) {
			continue
		}
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out
}

// Decay multiplies confidence of nodes older than halfLife; run sparingly.
func (s *Store) Decay(halfLife time.Duration, floor float64) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	changed := 0
	for k, n := range s.g.Nodes {
		age := now.Sub(n.UpdatedAt)
		if age <= halfLife {
			continue
		}
		factor := powHalf(float64(age) / float64(halfLife))
		n.Confidence *= factor
		if n.Confidence < floor {
			n.Confidence = floor
		}
		n.UpdatedAt = now
		s.g.Nodes[k] = n
		changed++
	}
	if changed > 0 {
		s.g.UpdatedAt = now
		return changed, s.persistLocked(Event{At: now, Op: "decay"})
	}
	return 0, nil
}

func powHalf(exp float64) float64 {
	// 0.5^exp, exp >= 1 here; repeated halving keeps it cheap and dependency-free.
	out := 1.0
	for i := 0; i < int(exp); i++ {
		out *= 0.5
	}
	return out
}

func (s *Store) persistLocked(ev Event) error {
	if err := s.writeGraph(); err != nil {
		return err
	}
	f, err := os.OpenFile(s.log, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open event log: %w", err)
	}
	defer f.Close()
	raw, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	if _, err := f.Write(append(raw, '\n')); err != nil {
		return fmt.Errorf("append event: %w", err)
	}
	return nil
}

func (s *Store) writeGraph() error {
	raw, err := json.MarshalIndent(s.g, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal graph: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("write graph temp: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("publish graph: %w", err)
	}
	return nil
}

func slug(s string) string {
	var b strings.Builder
	prev := false
	for _, r := range strings.ToLower(s) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prev = false
		case r == ':' || r == '|' || r == ' ' || r == '-' || r == '_' || r == '/':
			if !prev && b.Len() > 0 {
				b.WriteByte('-')
				prev = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func mergeMaps(a, b map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

func mergeStrings(a, b []string) []string {
	set := map[string]bool{}
	var out []string
	for _, v := range append(append([]string{}, a...), b...) {
		v = strings.TrimSpace(v)
		if v == "" || set[v] {
			continue
		}
		set[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func mergeSources(a, b []Source) []Source {
	seen := map[string]bool{}
	var out []Source
	for _, s := range append(append([]Source{}, a...), b...) {
		key := s.Kind + "|" + s.Ref + "|" + s.Quote
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, s)
	}
	return out
}
