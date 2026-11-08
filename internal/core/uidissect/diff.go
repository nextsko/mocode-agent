package uidissect

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Change is one difference between two maps.
type Change struct {
	Kind   string `json:"kind"` // added | removed | moved | resized | restyled | component
	Path   string `json:"path,omitempty"`
	Detail string `json:"detail,omitempty"`
	Before Rect   `json:"before,omitempty"`
	After  Rect   `json:"after,omitempty"`
}

// Diff is a box-level regression report between two dissections.
type Diff struct {
	BaseURL     string         `json:"base_url"`
	CurrentURL  string         `json:"current_url"`
	GeneratedAt time.Time      `json:"generated_at"`
	Summary     map[string]int `json:"summary"`
	Changes     []Change       `json:"changes"`
	Truncated   bool           `json:"truncated,omitempty"`
}

// Compare matches nodes by CSS path and reports geometric/style changes.
// Paths are the stable handle we already mint; a path that moved in the DOM
// shows up as removed+added, which is honest about what changed.
func Compare(base, cur Map) Diff {
	d := Diff{
		BaseURL:     base.URL,
		CurrentURL:  cur.URL,
		GeneratedAt: time.Now().UTC(),
		Summary:     map[string]int{},
	}

	baseByPath := make(map[string]Node, len(base.Nodes))
	for _, n := range base.Nodes {
		baseByPath[n.Path] = n
	}
	curByPath := make(map[string]Node, len(cur.Nodes))
	for _, n := range cur.Nodes {
		curByPath[n.Path] = n
	}

	paths := make([]string, 0, len(baseByPath)+len(curByPath))
	seen := map[string]bool{}
	for p := range baseByPath {
		paths = append(paths, p)
		seen[p] = true
	}
	for p := range curByPath {
		if !seen[p] {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)

	for _, p := range paths {
		b, inBase := baseByPath[p]
		c, inCur := curByPath[p]
		switch {
		case inBase && !inCur:
			d.add(Change{Kind: "removed", Path: p, Detail: fmt.Sprintf("%s removed", b.Tag), Before: b.Rect})
		case !inBase && inCur:
			d.add(Change{Kind: "added", Path: p, Detail: fmt.Sprintf("%s added", c.Tag), After: c.Rect})
		default:
			if abs(b.Rect.X-c.Rect.X) > 2 || abs(b.Rect.Y-c.Rect.Y) > 2 {
				d.add(Change{Kind: "moved", Path: p, Detail: fmt.Sprintf("moved (%s,%s) -> (%s,%s)", trimFloat(b.Rect.X), trimFloat(b.Rect.Y), trimFloat(c.Rect.X), trimFloat(c.Rect.Y)), Before: b.Rect, After: c.Rect})
			}
			if abs(b.Rect.W-c.Rect.W) > 2 || abs(b.Rect.H-c.Rect.H) > 2 {
				d.add(Change{Kind: "resized", Path: p, Detail: fmt.Sprintf("size %sx%s -> %sx%s", trimFloat(b.Rect.W), trimFloat(b.Rect.H), trimFloat(c.Rect.W), trimFloat(c.Rect.H)), Before: b.Rect, After: c.Rect})
			}
			if b.Styles.FontSize != c.Styles.FontSize || b.Styles.Color != c.Styles.Color || b.Styles.Background != c.Styles.Background {
				d.add(Change{Kind: "restyled", Path: p, Detail: styleDelta(b.Styles, c.Styles), Before: b.Rect, After: c.Rect})
			}
		}
	}

	// Component-level: fingerprint sets.
	baseComps := map[string]int{}
	for _, c := range base.Components {
		baseComps[fingerprint(base, c)]++
	}
	curComps := map[string]int{}
	for _, c := range cur.Components {
		curComps[fingerprint(cur, c)]++
	}
	for fp, n := range curComps {
		if baseComps[fp] < n {
			d.add(Change{Kind: "component", Detail: fmt.Sprintf("new component shape ×%d: %s", n-baseComps[fp], truncate(fp, 72))})
		}
	}
	for fp, n := range baseComps {
		if curComps[fp] < n {
			d.add(Change{Kind: "component", Detail: fmt.Sprintf("component shape disappeared ×%d: %s", n-curComps[fp], truncate(fp, 72))})
		}
	}
	return d
}

func (d *Diff) add(c Change) {
	if len(d.Changes) >= 300 {
		d.Truncated = true
		return
	}
	d.Summary[c.Kind]++
	d.Changes = append(d.Changes, c)
}

func styleDelta(a, b Styles) string {
	var parts []string
	if a.FontSize != b.FontSize {
		parts = append(parts, fmt.Sprintf("font-size %s -> %s", trimFloat(a.FontSize), trimFloat(b.FontSize)))
	}
	if a.Color != b.Color {
		parts = append(parts, fmt.Sprintf("color %s -> %s", a.Color, b.Color))
	}
	if a.Background != b.Background {
		parts = append(parts, fmt.Sprintf("background %s -> %s", a.Background, b.Background))
	}
	return strings.Join(parts, "; ")
}

// RenderDiff renders the report.
func RenderDiff(d Diff) string {
	var b strings.Builder
	b.WriteString("# UI 视觉回归\n\n")
	fmt.Fprintf(&b, "- Base: %s\n- Current: %s\n- Generated: %s\n\n", d.BaseURL, d.CurrentURL, d.GeneratedAt.Format(time.RFC3339))
	b.WriteString("## 汇总\n\n")
	kinds := make([]string, 0, len(d.Summary))
	for k := range d.Summary {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	if len(kinds) == 0 {
		b.WriteString("No differences — the two maps match.\n")
	} else {
		for _, k := range kinds {
			fmt.Fprintf(&b, "- %s: %d\n", k, d.Summary[k])
		}
	}
	b.WriteString("\n## 变化明细\n\n")
	for i, c := range d.Changes {
		if i >= 120 {
			b.WriteString("… (truncated)\n")
			break
		}
		fmt.Fprintf(&b, "- **%s**", c.Kind)
		if c.Path != "" {
			fmt.Fprintf(&b, " `%s`", truncate(c.Path, 100))
		}
		if c.Detail != "" {
			fmt.Fprintf(&b, " — %s", c.Detail)
		}
		b.WriteString("\n")
	}
	return b.String()
}
