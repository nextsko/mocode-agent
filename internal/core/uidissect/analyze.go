package uidissect

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Severity levels used by review findings.
const (
	SeverityError = "error"
	SeverityWarn  = "warn"
	SeverityInfo  = "info"
)

// Build analyses raw measurements into a map.
func Build(raw Raw) Map {
	m := Map{
		URL:         raw.URL,
		Title:       raw.Title,
		Viewport:    Rect{W: raw.Viewport.W, H: raw.Viewport.H},
		GeneratedAt: time.Now().UTC(),
		Nodes:       raw.Nodes,
	}
	m.Components = segment(raw.Nodes, raw.Viewport.W, raw.Viewport.H)
	m.Tokens = extractTokens(raw.Nodes)
	m.Findings = review(raw.Nodes, raw.Viewport.W)
	for _, n := range raw.Nodes {
		if n.Depth > m.Stats.Depth {
			m.Stats.Depth = n.Depth
		}
	}
	m.Stats.Nodes = len(raw.Nodes)
	m.Stats.Components = len(m.Components)
	m.Stats.Findings = len(m.Findings)
	m.Score = scoreOf(m.Findings)
	return m
}

// scoreOf turns findings into a 0-100 health score: errors weigh most.
func scoreOf(findings []Finding) int {
	score := 100
	for _, f := range findings {
		switch f.Severity {
		case SeverityError:
			score -= 8
		case SeverityWarn:
			score -= 3
		default:
			score--
		}
	}
	if score < 0 {
		return 0
	}
	return score
}

// segment finds landmarks, repeated siblings and large regions.
func segment(nodes []Node, vw, vh float64) []Component {
	if len(nodes) == 0 {
		return nil
	}
	// Parent index: nodes are in document order, so a depth stack recovers it.
	parent := make([]int, len(nodes))
	stack := []int{}
	children := make([]int, len(nodes))
	for i, n := range nodes {
		for len(stack) > 0 && nodes[stack[len(stack)-1]].Depth >= n.Depth {
			stack = stack[:len(stack)-1]
		}
		if len(stack) > 0 {
			parent[i] = stack[len(stack)-1]
			children[parent[i]]++
		} else {
			parent[i] = -1
		}
		stack = append(stack, i)
	}

	used := make([]bool, len(nodes))
	var out []Component
	add := func(i int, kind string, repeat int) {
		if used[i] {
			return
		}
		used[i] = true
		n := nodes[i]
		out = append(out, Component{
			ID:         fmt.Sprintf("c%d", len(out)+1),
			Kind:       kind,
			Path:       n.Path,
			Rect:       n.Rect,
			NodeCount:  countDescendants(nodes, parent, i, used == nil),
			Repeat:     repeat,
			TextSample: sampleText(nodes, parent, i),
		})
	}

	// Repeated siblings with the same shape become one re-usable slice.
	groups := map[string][]int{}
	for i, n := range nodes {
		if n.Depth == 0 || parent[i] < 0 {
			continue
		}
		key := fmt.Sprintf("%d|%s|%s", parent[i], n.Tag, strings.Join(n.Classes, "."))
		groups[key] = append(groups[key], i)
	}
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		idxs := groups[k]
		if len(idxs) < 3 || len(idxs) > 30 {
			continue
		}
		// Similar size: the boxes must agree within 30%.
		base := nodes[idxs[0]].Rect
		same := true
		for _, i := range idxs[1:] {
			r := nodes[i].Rect
			if abs(r.W-base.W) > base.W*0.3 || abs(r.H-base.H) > base.H*0.3 {
				same = false
				break
			}
		}
		if !same {
			continue
		}
		kind := "list-item"
		if hasClass(nodes[idxs[0]], "card") || strings.Contains(strings.Join(nodes[idxs[0]].Classes, " "), "card") {
			kind = "card"
		}
		add(idxs[0], kind, len(idxs))
		for _, i := range idxs[1:] {
			used[i] = true
		}
	}

	// Landmarks and large regions.
	landmarks := map[string]bool{"header": true, "nav": true, "main": true, "aside": true, "footer": true, "section": true, "article": true, "form": true, "dialog": true}
	for i, n := range nodes {
		if used[i] || n.Depth > 4 {
			continue
		}
		if landmarks[n.Tag] && (vw == 0 || (n.Rect.W >= vw*0.15 && n.Rect.H >= vh*0.02)) {
			add(i, n.Tag, 0)
			continue
		}
		if n.Depth <= 1 && (vw == 0 || n.Rect.W*n.Rect.H >= vw*vh*0.03) {
			add(i, "region", 0)
		}
	}
	return out
}

func countDescendants(_ []Node, parent []int, idx int, all bool) int {
	_ = all
	n := 0
	for i := range parent {
		p := parent[i]
		for p >= 0 {
			if p == idx {
				n++
				break
			}
			p = parent[p]
		}
	}
	if n == 0 {
		return 1
	}
	return n + 1
}

func sampleText(nodes []Node, parent []int, idx int) string {
	for i := range nodes {
		p := parent[i]
		for p >= 0 {
			if p == idx {
				if t := strings.TrimSpace(nodes[i].Text); t != "" {
					return t
				}
				break
			}
			p = parent[p]
		}
	}
	return ""
}

func hasClass(n Node, c string) bool {
	for _, x := range n.Classes {
		if strings.EqualFold(x, c) {
			return true
		}
	}
	return false
}

func extractTokens(nodes []Node) Tokens {
	fonts := map[float64]bool{}
	spacing := map[float64]bool{}
	colors := map[string]bool{}
	radii := map[string]bool{}
	shadows := 0
	for _, n := range nodes {
		s := n.Styles
		if s.FontSize > 0 {
			fonts[round1(s.FontSize)] = true
		}
		for _, v := range []float64{s.MarginTop, s.MarginBot, s.PadTop, s.PadBot, s.MarginLeft, s.PadLeft} {
			if v > 0 {
				spacing[round1(v)] = true
			}
		}
		if s.Color != "" && !isTransparent(s.Color) {
			colors[s.Color] = true
		}
		if s.Background != "" && !isTransparent(s.Background) {
			colors[s.Background] = true
		}
		if s.Radius != "" {
			radii[s.Radius] = true
		}
		if s.Shadow {
			shadows++
		}
	}
	return Tokens{
		FontSizes: sortedFloats(fonts),
		Spacing:   sortedFloats(spacing),
		Colors:    sortedStrings(colors, 24),
		Radii:     sortedStrings(radii, 12),
		Shadows:   shadows,
	}
}

func review(nodes []Node, vw float64) []Finding {
	var out []Finding
	perCat := map[string]int{}
	add := func(f Finding) {
		if perCat[f.Category] >= 8 || len(out) >= 48 {
			return
		}
		perCat[f.Category]++
		out = append(out, f)
	}

	// Typography.
	var sizes []float64
	for _, n := range nodes {
		if n.Styles.FontSize > 0 && n.Text != "" {
			sizes = append(sizes, round1(n.Styles.FontSize))
		}
	}
	distinct := uniqueSorted(sizes)
	if len(distinct) > 8 {
		add(Finding{Category: "typography", Severity: SeverityWarn, Message: fmt.Sprintf("%d distinct text sizes — the type scale is drifting.", len(distinct)), Suggestion: "Normalize to a 4–6 step scale (e.g. 12/14/16/20/28)."})
	}
	if len(distinct) > 0 && distinct[0] < 12 && distinct[0] > 0 {
		add(Finding{Category: "typography", Severity: SeverityWarn, Message: fmt.Sprintf("Smallest text is %s px, below comfortable reading size.", trimFloat(distinct[0])), Suggestion: "Raise body text to at least 12–14 px."})
	}

	// Spacing rhythm.
	var gaps []float64
	for _, n := range nodes {
		for _, v := range []float64{n.Styles.MarginTop, n.Styles.MarginBot, n.Styles.PadTop, n.Styles.PadBot, n.Styles.MarginLeft, n.Styles.PadLeft} {
			if v > 0 {
				gaps = append(gaps, round1(v))
			}
		}
	}
	if distinctGaps := uniqueSorted(gaps); len(distinctGaps) > 10 {
		add(Finding{Category: "spacing", Severity: SeverityWarn, Message: fmt.Sprintf("%d distinct spacing values — layout rhythm is inconsistent.", len(distinctGaps)), Suggestion: "Adopt an 4/8 px spacing scale and replace ad-hoc values."})
	}

	// Colors.
	colors := map[string]bool{}
	for _, n := range nodes {
		if c := n.Styles.Color; c != "" && !isTransparent(c) {
			colors[c] = true
		}
		if c := n.Styles.Background; c != "" && !isTransparent(c) {
			colors[c] = true
		}
	}
	if len(colors) > 20 {
		add(Finding{Category: "color", Severity: SeverityWarn, Message: fmt.Sprintf("%d distinct colors on one page.", len(colors)), Suggestion: "Collapse to a palette: neutral ramp + 1 accent + semantic states."})
	}

	// Contrast (text against the nearest ancestor background).
	for i, n := range nodes {
		if n.Text == "" || n.Styles.FontSize < 10 {
			continue
		}
		bg := nearestBackground(nodes, i)
		if bg == "" || isTransparent(bg) {
			continue
		}
		if ratio, ok := contrastRatio(n.Styles.Color, bg); ok && ratio < 4.5 {
			add(Finding{Category: "contrast", Severity: SeverityError, Message: fmt.Sprintf("Text %q has contrast %.1f:1 against its background.", truncate(n.Text, 28), ratio), Selector: n.Path, Suggestion: "Darken the text or lighten the surface until the ratio is >= 4.5:1."})
		}
	}

	// Touch targets.
	for _, n := range nodes {
		if !n.Interactive || n.Disabled || (n.Rect.W == 0 && n.Rect.H == 0) {
			continue
		}
		if n.Rect.W < 32 || n.Rect.H < 32 {
			add(Finding{Category: "touch-target", Severity: SeverityWarn, Message: fmt.Sprintf("Interactive %q is %sx%s px.", truncate(firstNonEmpty(n.AriaLabel, n.Text, n.Tag), 28), trimFloat(n.Rect.W), trimFloat(n.Rect.H)), Selector: n.Path, Suggestion: "Give it at least a 44x44 px hit area (padding or a pseudo-element)."})
		}
	}

	// Clipping and missing labels.
	for _, n := range nodes {
		if n.Styles.OverflowX {
			add(Finding{Category: "overflow", Severity: SeverityWarn, Message: fmt.Sprintf("%s clips horizontally (scrollWidth > clientWidth).", n.Path), Selector: n.Path, Suggestion: "Allow wrapping, ellipsis, or a responsive layout."})
		}
		if n.Tag == "img" && n.ImgAlt == "" {
			add(Finding{Category: "a11y", Severity: SeverityWarn, Message: "Image without alt text.", Selector: n.Path, Suggestion: "Add alt, or alt=\"\" if it is decorative."})
		}
		if (n.Tag == "button" || n.Tag == "a") && strings.TrimSpace(n.Text) == "" && strings.TrimSpace(n.AriaLabel) == "" {
			add(Finding{Category: "a11y", Severity: SeverityError, Message: fmt.Sprintf("%s has no accessible label.", n.Tag), Selector: n.Path, Suggestion: "Add visible text or aria-label."})
		}
	}

	// Heading structure.
	h1 := 0
	for _, n := range nodes {
		if n.Tag == "h1" {
			h1++
		}
	}
	if h1 > 1 {
		add(Finding{Category: "hierarchy", Severity: SeverityInfo, Message: fmt.Sprintf("%d h1 elements on the page.", h1), Suggestion: "Keep one h1 as the page title; demote the rest."})
	}

	// Layout balance: too many full-width boxes stacked.
	full := 0
	for _, n := range nodes {
		if n.Depth >= 2 && n.Rect.W >= vw*0.95 {
			full++
		}
	}
	if full > 0 && full == len(nodes) {
		add(Finding{Category: "layout", Severity: SeverityInfo, Message: "Every measured box is full-width; the layout is a single column.", Suggestion: "Use a grid or max-width container if this is not intentional."})
	}

	sort.SliceStable(out, func(i, j int) bool {
		rank := map[string]int{"error": 0, "warn": 1, "info": 2}
		return rank[out[i].Severity] < rank[out[j].Severity]
	})
	return out
}

func nearestBackground(nodes []Node, idx int) string {
	depth := nodes[idx].Depth
	for i := idx; i >= 0; i-- {
		if nodes[i].Depth < depth && nodes[i].Styles.Background != "" && !isTransparent(nodes[i].Styles.Background) {
			return nodes[i].Styles.Background
		}
		if nodes[i].Depth < depth {
			depth = nodes[i].Depth
		}
	}
	return ""
}

// contrastRatio computes WCAG contrast for CSS colors in rgb()/rgba()/hex form.
func contrastRatio(fg, bg string) (float64, bool) {
	f, ok1 := parseColor(fg)
	b, ok2 := parseColor(bg)
	if !ok1 || !ok2 {
		return 0, false
	}
	l1, l2 := luminance(f), luminance(b)
	if l1 < l2 {
		l1, l2 = l2, l1
	}
	return (l1 + 0.05) / (l2 + 0.05), true
}

type rgb struct{ r, g, b float64 }

func parseColor(s string) (rgb, bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	if strings.HasPrefix(s, "#") {
		h := strings.TrimPrefix(s, "#")
		if len(h) == 3 {
			h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
		}
		if len(h) != 6 {
			return rgb{}, false
		}
		v, err := strconv.ParseUint(h, 16, 32)
		if err != nil {
			return rgb{}, false
		}
		return rgb{float64((v >> 16) & 0xff), float64((v >> 8) & 0xff), float64(v & 0xff)}, true
	}
	if strings.HasPrefix(s, "rgb") {
		i := strings.Index(s, "(")
		j := strings.LastIndex(s, ")")
		if i < 0 || j <= i {
			return rgb{}, false
		}
		parts := strings.Split(s[i+1:j], ",")
		if len(parts) < 3 {
			return rgb{}, false
		}
		var vals [3]float64
		for k := 0; k < 3; k++ {
			v, err := strconv.ParseFloat(strings.TrimSpace(parts[k]), 64)
			if err != nil {
				return rgb{}, false
			}
			vals[k] = v
		}
		return rgb{vals[0], vals[1], vals[2]}, true
	}
	return rgb{}, false
}

func luminance(c rgb) float64 {
	ch := func(v float64) float64 {
		v /= 255
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*ch(c.r) + 0.7152*ch(c.g) + 0.0722*ch(c.b)
}

func isTransparent(c string) bool {
	c = strings.ToLower(strings.ReplaceAll(c, " ", ""))
	return c == "" || c == "transparent" || strings.Contains(c, "rgba(0,0,0,0)")
}

func round1(v float64) float64 { return float64(int(v*10+0.5)) / 10 }

func uniqueSorted(in []float64) []float64 {
	seen := map[float64]bool{}
	var out []float64
	for _, v := range in {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Float64s(out)
	return out
}

func sortedFloats(set map[float64]bool) []float64 {
	out := make([]float64, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Float64s(out)
	return out
}

func sortedStrings(set map[string]bool, limit int) []string {
	out := make([]string, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Strings(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func trimFloat(v float64) string {
	if v == float64(int(v)) {
		return strconv.Itoa(int(v))
	}
	return strconv.FormatFloat(v, 'f', 1, 64)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
