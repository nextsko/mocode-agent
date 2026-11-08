package uidissect

import (
	"fmt"
	"sort"
	"strings"
)

// RenderMarkdown renders the dissection as a "庖丁解牛" map: structure first,
// then the component slices, the aesthetic review and the design tokens.
func RenderMarkdown(m Map) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# 庖丁解牛 · %s\n\n", firstNonEmpty(m.Title, m.URL))
	fmt.Fprintf(&b, "- URL: %s\n", m.URL)
	fmt.Fprintf(&b, "- Viewport: %s x %s\n", trimFloat(m.Viewport.W), trimFloat(m.Viewport.H))
	fmt.Fprintf(&b, "- Nodes: %d · Components: %d · Findings: %d · Depth: %d\n\n",
		m.Stats.Nodes, m.Stats.Components, m.Stats.Findings, m.Stats.Depth)

	b.WriteString("## 结构地图\n\n```text\n")
	lines := 0
	for _, n := range m.Nodes {
		if lines >= 160 {
			b.WriteString("… (truncated)\n")
			break
		}
		label := n.Tag
		if n.ID != "" {
			label += "#" + n.ID
		}
		if len(n.Classes) > 0 {
			label += "." + strings.Join(n.Classes[:min(3, len(n.Classes))], ".")
		}
		text := truncate(strings.TrimSpace(n.Text), 48)
		if text != "" {
			text = " \"" + text + "\""
		}
		fmt.Fprintf(&b, "%s%s%s @%s,%s %sx%s\n",
			strings.Repeat("  ", n.Depth), label, text,
			trimFloat(n.Rect.X), trimFloat(n.Rect.Y), trimFloat(n.Rect.W), trimFloat(n.Rect.H))
		lines++
	}
	b.WriteString("```\n\n")

	b.WriteString("## 组件切片\n\n")
	if len(m.Components) == 0 {
		b.WriteString("_No components segmented._\n\n")
	} else {
		b.WriteString("| ID | Kind | Box | Nodes | Repeat | Sample |\n|---|---|---|---|---|---|\n")
		for _, c := range m.Components {
			fmt.Fprintf(&b, "| %s | %s | %s,%s %sx%s | %d | %d | %s |\n",
				c.ID, c.Kind, trimFloat(c.Rect.X), trimFloat(c.Rect.Y), trimFloat(c.Rect.W), trimFloat(c.Rect.H),
				c.NodeCount, c.Repeat, truncate(strings.ReplaceAll(c.TextSample, "|", "\\|"), 40))
		}
		b.WriteString("\n")
	}

	b.WriteString("## 审美审查\n\n")
	if len(m.Findings) == 0 {
		b.WriteString("_No findings — the measured surface is consistent._\n\n")
	} else {
		for _, f := range m.Findings {
			icon := map[string]string{SeverityError: "🔴", SeverityWarn: "🟠", SeverityInfo: "🔵"}[f.Severity]
			fmt.Fprintf(&b, "- %s **[%s]** %s", icon, f.Category, f.Message)
			if f.Selector != "" {
				fmt.Fprintf(&b, " `%s`", truncate(f.Selector, 80))
			}
			if f.Suggestion != "" {
				fmt.Fprintf(&b, " — %s", f.Suggestion)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("## 设计令牌\n\n")
	writeTokenLine(&b, "Font sizes", floatsToStrings(m.Tokens.FontSizes), "px")
	writeTokenLine(&b, "Spacing", floatsToStrings(m.Tokens.Spacing), "px")
	writeTokenLine(&b, "Radii", m.Tokens.Radii, "")
	writeTokenLine(&b, "Colors", m.Tokens.Colors, "")
	fmt.Fprintf(&b, "- Shadows: %d\n\n", m.Tokens.Shadows)

	b.WriteString("## 复刻入口\n\n")
	b.WriteString("Use `action=replicate` with a component id above to emit a semantic HTML+CSS skeleton, then refine it against the map.\n")
	return b.String()
}

func writeTokenLine(b *strings.Builder, label string, vals []string, suffix string) {
	if len(vals) == 0 {
		fmt.Fprintf(b, "- %s: (none)\n", label)
		return
	}
	for i := range vals {
		vals[i] += suffix
	}
	fmt.Fprintf(b, "- %s: %s\n", label, strings.Join(vals, ", "))
}

func floatsToStrings(in []float64) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = trimFloat(v)
	}
	return out
}

// RenderJSON returns the machine-readable map; callers persist it next to the
// markdown so a later `replicate` can work without re-measuring.
func RenderJSON(m Map) ([]byte, error) {
	return jsonMarshalIndent(m)
}

// TopComponents returns components ordered by node count, for recommendations.
func TopComponents(m Map, n int) []Component {
	out := append([]Component{}, m.Components...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].NodeCount > out[j].NodeCount })
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
