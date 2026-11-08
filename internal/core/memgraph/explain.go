package memgraph

import (
	"fmt"
	"sort"
	"strings"
)

// Explain returns evidence chains for a node: paths through the graph that
// end at evidence (sources), so a belief can be traced instead of asserted.
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
		path  []string
		kinds []string
		conf  float64
	}
	var out []EvidenceChain
	visited := map[string]bool{id: true}
	queue := []frame{{path: []string{target.Name}, conf: target.Confidence}}
	for depth := 0; depth < maxDepth && len(queue) > 0; depth++ {
		var next []frame
		for _, f := range queue {
			curID := nodeIDByName(s.g, f.path[len(f.path)-1])
			if curID == "" {
				continue
			}
			for _, e := range s.g.Edges {
				if e.From != curID && e.To != curID {
					continue
				}
				other := e.To
				if e.To == curID {
					other = e.From
				}
				if visited[other] {
					continue
				}
				n := s.g.Nodes[other]
				nf := frame{
					path:  append(append([]string{}, f.path...), n.Name),
					kinds: append(append([]string{}, f.kinds...), e.Kind),
					conf:  minFloat(f.conf, e.Confidence, n.Confidence),
				}
				if len(n.Sources) > 0 {
					visited[other] = true
					out = append(out, EvidenceChain{Path: nf.path, EdgeKinds: nf.kinds, Sources: n.Sources, Confidence: nf.conf})
					continue
				}
				visited[other] = true
				next = append(next, nf)
			}
		}
		queue = next
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Confidence > out[j].Confidence })
	if len(out) > 8 {
		out = out[:8]
	}
	return out
}

func nodeIDByName(g Graph, name string) string {
	for id, n := range g.Nodes {
		if n.Name == name {
			return id
		}
	}
	return ""
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

// Conflicts finds same-subject relationships that point at different objects
// (excluding supersession, which is an intentional evolution).
func (s *Store) Conflicts() []Conflict {
	s.mu.Lock()
	defer s.mu.Unlock()
	byKey := map[string][]Edge{}
	for _, e := range s.g.Edges {
		if e.Kind == EdgeSupersedes || e.Kind == EdgeEvidencedBy {
			continue
		}
		key := e.From + "|" + e.Kind
		byKey[key] = append(byKey[key], e)
	}
	var out []Conflict
	for _, edges := range byKey {
		if len(edges) < 2 {
			continue
		}
		seen := map[string]bool{}
		for i := range edges {
			for j := i + 1; j < len(edges); j++ {
				if edges[i].To == edges[j].To {
					continue
				}
				key := edges[i].To + "|" + edges[j].To
				if seen[key] {
					continue
				}
				seen[key] = true
				a, b := s.g.Nodes[edges[i].To], s.g.Nodes[edges[j].To]
				out = append(out, Conflict{
					A: a.Name, B: b.Name, Subject: s.g.Nodes[edges[i].From].Name,
					Kind:   edges[i].Kind,
					Detail: fmt.Sprintf("%s %s both %q and %q", s.g.Nodes[edges[i].From].Name, edges[i].Kind, a.Name, b.Name),
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
