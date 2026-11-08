package memgraph

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// RenderMermaid renders the graph for visualization.
func RenderMermaid(g Graph) string {
	var b strings.Builder
	b.WriteString("graph LR\n")
	ids := make([]string, 0, len(g.Nodes))
	for id := range g.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		n := g.Nodes[id]
		fmt.Fprintf(&b, "  %s[\"%s\\n%s %.2f\\nid=%s\"]\n", safeID(id), escape(n.Name), n.Kind, n.Confidence, id)
	}
	edgeIDs := make([]string, 0, len(g.Edges))
	for id := range g.Edges {
		edgeIDs = append(edgeIDs, id)
	}
	sort.Strings(edgeIDs)
	for _, id := range edgeIDs {
		e := g.Edges[id]
		fmt.Fprintf(&b, "  %s -->|%s| %s\n", safeID(e.From), e.Kind, safeID(e.To))
	}
	return b.String()
}

// ExportBundle writes the graph, a Mermaid file and a standalone HTML viewer.
func (s *Store) ExportBundle(workingDir string) (string, error) {
	g := s.Graph()
	dir := filepath.Join(workingDir, RootDir, "exports", time.Now().UTC().Format("20060102T150405"))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("create export dir: %w", err)
	}
	raw, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal graph: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "graph.json"), raw, 0o600); err != nil {
		return "", fmt.Errorf("write graph json: %w", err)
	}
	mmd := RenderMermaid(g)
	if err := os.WriteFile(filepath.Join(dir, "graph.mmd"), []byte(mmd), 0o600); err != nil {
		return "", fmt.Errorf("write mermaid: %w", err)
	}
	html := fmt.Sprintf(`<!doctype html><meta charset="utf-8"><title>mocode memory graph</title>
<script src="https://cdn.jsdelivr.net/npm/mermaid@10/dist/mermaid.min.js"></script>
<body style="background:#0b0d10;color:#e6e6e6;font-family:system-ui">
<h3>mocode memory graph</h3><pre class="mermaid">%s</pre>
<script>mermaid.initialize({startOnLoad:true,theme:'dark'});</script></body>`, htmlEscape(mmd))
	if err := os.WriteFile(filepath.Join(dir, "graph.html"), []byte(html), 0o600); err != nil {
		return "", fmt.Errorf("write html: %w", err)
	}
	return dir, nil
}

func safeID(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

func escape(s string) string { return strings.ReplaceAll(s, `"`, `'`) }

func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}
