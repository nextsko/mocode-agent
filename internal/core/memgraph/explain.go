package memgraph

import (
	"fmt"
	"sort"
	"strings"
)

// Explain returns evidence chains for a node: paths through the graph that
// end at evidence (sources), so a belief can be traced instead of asserted.
// Paths carry node IDs, never names, so duplicate names cannot cross-link.
func (s *Store) Explain(id string) []EvidenceChain {
	s.mu.Lock()
	defer s.mu.Unlock()
	target, ok := s.g.Nodes[id]
	if !ok {
		return nil
	}
	if len(target.Sources) > 0 {
		return []EvidenceChain{{Path: []string{target.Name}, Sources: target.Sources, Confidence: target.Confidence}}
	}

	const maxDepth = 4
	type frame struct {
		ids   []string
		names []string
		kinds []string
		conf  float64
		seen  map[string]bool
	}
	start := frame{
		ids: []string{id}, names: []string{target.Name}, conf: target.Confidence,
		seen: map[string]bool{id: true},
	}
	var out []EvidenceChain
	queue := []frame{start}
	for depth := 0; depth < maxDepth && len(queue) > 0; depth++ {
		var next []frame
		for _, f := range queue {
			cur := f.ids[len(f.ids)-1]
			for _, e := range sortedEdges(s.g.Edges) {
				if e.From != cur && e.To != cur {
					continue
				}
				other := e.To
				if e.To == cur {
					other = e.From
				}
				if f.seen[other] {
					continue
				}
				n, ok := s.g.Nodes[other]
				if !ok {
					continue
				}
				nf := frame{
					ids:   append(append([]string{}, f.ids...), other),
					names: append(append([]string{}, f.names...), n.Name),
					kinds: append(append([]string{}, f.kinds...), e.Kind),
					conf:  minFloat(f.conf, e.Confidence, n.Confidence),
					seen:  copySeen(f.seen, other),
				}
				if len(n.Sources) > 0 {
					out = append(out, EvidenceChain{Path: nf.names, EdgeKinds: nf.kinds, Sources: n.Sources, Confidence: nf.conf})
					continue
				}
				next = append(next, nf)
			}
		}
		queue = next
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Confidence != out[j].Confidence {
			return out[i].Confidence > out[j].Confidence
		}
		return strings.Join(out[i].Path, ">") < strings.Join(out[j].Path, ">")
	})
	if len(out) > 8 {
		out = out[:8]
	}
	return out
}

func sortedEdges(edges map[string]Edge) []Edge {
	ids := make([]string, 0, len(edges))
	for id := range edges {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]Edge, 0, len(ids))
	for _, id := range ids {
		out = append(out, edges[id])
	}
	return out
}

func copySeen(in map[string]bool, add string) map[string]bool {
	out := make(map[string]bool, len(in)+1)
	for k := range in {
		out[k] = true
	}
	out[add] = true
	return out
}

func minFloat(vals ...float64) float64 {
	m := 1.0
	for _, v := range vals {
		if v > 0 && v < m {
			m = v
		}
	}
	return m
}

// Conflict describes two mutually inconsistent beliefs.
type Conflict struct {
	A       string `json:"a"`
	B       string `json:"b"`
	Subject string `json:"subject"`
	Kind    string `json:"kind"`
	Detail  string `json:"detail"`
}

// functionalKinds are relationships that can hold only one value per subject.
// Multi-valued relations such as prefers/relates_to are deliberately absent:
// a user may prefer many things without that being a contradiction.
var functionalKinds = map[string]bool{
	"timezone": true, "language": true, "name": true, "role": true,
	"location": true, "status": true, "current": true, "canonical": true,
}

// Conflicts finds genuinely inconsistent beliefs: functional relationships
// that point at two different values, and facts with the same key but
// different values. Supersession is intentional evolution, not conflict.
func (s *Store) Conflicts() []Conflict {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conflictsLocked()
}

func (s *Store) conflictsLocked() []Conflict {
	byKey := map[string][]Edge{}
	for _, e := range s.g.Edges {
		if e.Kind == EdgeSupersedes || e.Kind == EdgeEvidencedBy || !functionalKinds[e.Kind] {
			continue
		}
		byKey[e.From+"|"+e.Kind] = append(byKey[e.From+"|"+e.Kind], e)
	}
	var out []Conflict
	for _, edges := range byKey {
		for i := 0; i < len(edges); i++ {
			for j := i + 1; j < len(edges); j++ {
				if edges[i].To == edges[j].To {
					continue
				}
				a, b := s.g.Nodes[edges[i].To], s.g.Nodes[edges[j].To]
				out = append(out, Conflict{
					A: a.Name, B: b.Name, Subject: s.g.Nodes[edges[i].From].Name, Kind: edges[i].Kind,
					Detail: fmt.Sprintf("%s %s both %q and %q", s.g.Nodes[edges[i].From].Name, edges[i].Kind, a.Name, b.Name),
				})
			}
		}
	}
	// Facts with the same key but different values are contradictions too.
	facts := map[string][]Node{}
	for _, n := range s.g.Nodes {
		if n.Kind != KindFact || n.SupersededBy != "" {
			continue
		}
		key := strings.ToLower(n.Name)
		if n.Props != nil {
			if k, ok := n.Props["key"].(string); ok && k != "" {
				key = strings.ToLower(k)
			}
		}
		facts[key] = append(facts[key], n)
	}
	for key, group := range facts {
		if len(group) < 2 {
			continue
		}
		seen := map[string]bool{}
		for i := 0; i < len(group); i++ {
			for j := i + 1; j < len(group); j++ {
				va := fmt.Sprint(group[i].Props["value"])
				vb := fmt.Sprint(group[j].Props["value"])
				if va == vb || seen[va+"|"+vb] {
					continue
				}
				seen[va+"|"+vb] = true
				out = append(out, Conflict{
					A: group[i].Name, B: group[j].Name, Subject: key, Kind: "fact",
					Detail: fmt.Sprintf("fact %q is both %q and %q", key, va, vb),
				})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Subject < out[j].Subject })
	return out
}

// RenderExplain renders evidence chains for humans.
func RenderExplain(id string, chains []EvidenceChain) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# 为什么我相信 %s\n\n", id)
	if len(chains) == 0 {
		b.WriteString("_No evidence chain found — this belief is unsupported._\n")
		return b.String()
	}
	for i, c := range chains {
		fmt.Fprintf(&b, "%d. %s", i+1, strings.Join(c.Path, " → "))
		if len(c.EdgeKinds) > 0 {
			fmt.Fprintf(&b, "  _(%s)_", strings.Join(c.EdgeKinds, " / "))
		}
		fmt.Fprintf(&b, "  confidence %.2f\n", c.Confidence)
		for _, src := range c.Sources {
			fmt.Fprintf(&b, "   - evidence [%s] %s: %s\n", src.Kind, src.Ref, src.Quote)
		}
	}
	return b.String()
}
