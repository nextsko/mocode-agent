package uidissect

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// System is a design system mined from saved maps: which component shapes
// recur across pages, and which tokens the site actually uses.
type System struct {
	GeneratedAt time.Time `json:"generated_at"`
	Pages       int       `json:"pages"`
	Clusters    []Cluster `json:"clusters"`
	Tokens      Tokens    `json:"tokens"`
}

// Cluster is one recurring component shape.
type Cluster struct {
	Fingerprint string   `json:"fingerprint"`
	Kind        string   `json:"kind"`
	Count       int      `json:"count"`
	Pages       []string `json:"pages"`
	AvgW        float64  `json:"avg_w"`
	AvgH        float64  `json:"avg_h"`
	Examples    []string `json:"examples,omitempty"`
}

// SystemFrom mines recurring component shapes across maps.
func SystemFrom(maps []Map) System {
	type agg struct {
		kinds    map[string]int
		pages    map[string]bool
		examples []string
		w, h     float64
		n        int
	}
	groups := map[string]*agg{}
	allNodes := make([]Node, 0)
	for _, m := range maps {
		allNodes = append(allNodes, m.Nodes...)
		for _, c := range m.Components {
			fp := fingerprint(m, c)
			a := groups[fp]
			if a == nil {
				a = &agg{kinds: map[string]int{}, pages: map[string]bool{}}
				groups[fp] = a
			}
			a.kinds[c.Kind]++
			a.pages[firstNonEmpty(m.URL, m.Title)] = true
			a.w += c.Rect.W
			a.h += c.Rect.H
			a.n++
			if len(a.examples) < 3 {
				if s := strings.TrimSpace(c.TextSample); s != "" {
					a.examples = append(a.examples, s)
				}
			}
		}
	}

	s := System{GeneratedAt: time.Now().UTC(), Pages: len(maps), Tokens: extractTokens(allNodes)}
	for fp, a := range groups {
		kind := "mixed"
		best := 0
		for k, n := range a.kinds {
			if n > best {
				best, kind = n, k
			}
		}
		pages := make([]string, 0, len(a.pages))
		for p := range a.pages {
			pages = append(pages, p)
		}
		sort.Strings(pages)
		s.Clusters = append(s.Clusters, Cluster{
			Fingerprint: fp,
			Kind:        kind,
			Count:       a.n,
			Pages:       pages,
			AvgW:        round1(a.w / float64(a.n)),
			AvgH:        round1(a.h / float64(a.n)),
			Examples:    a.examples,
		})
	}
	sort.SliceStable(s.Clusters, func(i, j int) bool {
		if s.Clusters[i].Count != s.Clusters[j].Count {
			return s.Clusters[i].Count > s.Clusters[j].Count
		}
		return s.Clusters[i].Fingerprint < s.Clusters[j].Fingerprint
	})
	return s
}

// fingerprint describes a component's structural shape: tag sequence plus
// coarse style buckets. Text and exact pixels are excluded so the same card
// clusters across pages even when its copy changes.
func fingerprint(m Map, comp Component) string {
	start := -1
	for i, n := range m.Nodes {
		if n.Path == comp.Path {
			start = i
			break
		}
	}
	if start < 0 {
		return comp.Kind + "|unknown"
	}
	depth := m.Nodes[start].Depth
	var tags []string
	var fontBucket, radiusBucket string
	for i := start; i < len(m.Nodes); i++ {
		n := m.Nodes[i]
		if i > start && n.Depth <= depth {
			break
		}
		tags = append(tags, n.Tag)
		if fontBucket == "" && n.Styles.FontSize > 0 {
			fontBucket = trimFloat(round1(n.Styles.FontSize/2) * 2)
		}
		if radiusBucket == "" && n.Styles.Radius != "" {
			radiusBucket = n.Styles.Radius
		}
		if len(tags) >= 24 {
			break
		}
	}
	return fmt.Sprintf("%s|%s|fs:%s|r:%s", comp.Kind, strings.Join(tags, ">"), fontBucket, radiusBucket)
}

// LoadSystem reads the persisted design system, if any.
func LoadSystem(workingDir string) (System, bool) {
	path := filepath.Join(Dir(workingDir), "system.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return System{}, false
	}
	var s System
	if err := jsonUnmarshal(raw, &s); err != nil {
		return System{}, false
	}
	return s, true
}

// SaveSystem persists the mined design system.
func SaveSystem(s System, workingDir string) (string, error) {
	dir := Dir(workingDir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("create ui dir: %w", err)
	}
	raw, err := jsonMarshalIndent(s)
	if err != nil {
		return "", fmt.Errorf("marshal system: %w", err)
	}
	path := filepath.Join(dir, "system.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return "", fmt.Errorf("write system: %w", err)
	}
	md := filepath.Join(dir, "system.md")
	if err := os.WriteFile(md, []byte(RenderSystem(s)), 0o600); err != nil {
		return "", fmt.Errorf("write system markdown: %w", err)
	}
	return md, nil
}

// LoadMaps reads every saved map JSON next to the system.
func LoadMaps(workingDir string) ([]Map, error) {
	entries, err := os.ReadDir(Dir(workingDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read ui dir: %w", err)
	}
	var maps []Map
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		switch e.Name() {
		case "system.json", "brand.json", "taste.json":
			continue
		}
		raw, err := os.ReadFile(filepath.Join(Dir(workingDir), e.Name()))
		if err != nil {
			continue
		}
		var m Map
		if err := jsonUnmarshal(raw, &m); err != nil {
			continue
		}
		if m.GeneratedAt.IsZero() || (len(m.Nodes) == 0 && m.URL == "") {
			continue
		}
		maps = append(maps, m)
	}
	sort.SliceStable(maps, func(i, j int) bool { return maps[i].GeneratedAt.Before(maps[j].GeneratedAt) })
	return maps, nil
}

// RenderSystem renders the mined design system as markdown.
func RenderSystem(s System) string {
	var b strings.Builder
	b.WriteString("# 站点设计系统\n\n")
	fmt.Fprintf(&b, "- Pages: %d\n- Clusters: %d\n\n", s.Pages, len(s.Clusters))
	b.WriteString("## 组件聚类\n\n")
	if len(s.Clusters) == 0 {
		b.WriteString("_No recurring components yet — run `ui_dissect action=map` on more pages._\n")
	} else {
		b.WriteString("| Kind | Count | Pages | Avg box | Shape | Examples |\n|---|---|---|---|---|---|\n")
		for _, c := range s.Clusters {
			fmt.Fprintf(&b, "| %s | %d | %d | %sx%s | `%s` | %s |\n",
				c.Kind, c.Count, len(c.Pages), trimFloat(c.AvgW), trimFloat(c.AvgH),
				truncate(c.Fingerprint, 48), truncate(strings.Join(c.Examples, " / "), 40))
		}
	}
	b.WriteString("\n## 站点令牌\n\n")
	writeTokenLine(&b, "Font sizes", floatsToStrings(s.Tokens.FontSizes), "px")
	writeTokenLine(&b, "Spacing", floatsToStrings(s.Tokens.Spacing), "px")
	writeTokenLine(&b, "Colors", s.Tokens.Colors, "")
	return b.String()
}
