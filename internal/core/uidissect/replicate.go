package uidissect

import (
	"fmt"
	"strings"
)

// Skeleton emits a semantic HTML + CSS skeleton for one component, using the
// measured boxes and styles. It is a starting point for replication, not a
// pixel-perfect rebuild: the map stays the source of truth.
func Skeleton(m Map, comp Component) (htmlOut, cssOut string) {
	start := -1
	for i, n := range m.Nodes {
		if n.Path == comp.Path {
			start = i
			break
		}
	}
	if start < 0 {
		return "", ""
	}
	depth := m.Nodes[start].Depth
	var subtree []Node
	for i := start; i < len(m.Nodes); i++ {
		n := m.Nodes[i]
		if i > start && n.Depth <= depth {
			break
		}
		subtree = append(subtree, n)
		if len(subtree) >= 200 {
			break
		}
	}

	var html strings.Builder
	var css strings.Builder
	css.WriteString(cssTokens(m.Tokens))
	fmt.Fprintf(&html, "<section class=\"component component-%s\">\n", comp.ID)

	open := 1 // the section above
	for _, n := range subtree {
		level := n.Depth - depth + 1
		for open > level {
			html.WriteString(strings.Repeat("  ", open) + "</div>\n")
			open--
		}
		class := "c-" + slugify(n.Tag+"-"+n.ID+"-"+strings.Join(n.Classes, "-"))
		if len(class) > 64 {
			class = class[:64]
		}
		fmt.Fprintf(&css, ".%s { %s }\n", class, cssRule(n.Styles))
		indent := strings.Repeat("  ", level)
		tag := n.Tag
		switch tag {
		case "img":
			fmt.Fprintf(&html, "%s<img class=\"%s\" src=\"\" alt=%q>\n", indent, class, n.ImgAlt)
			continue
		case "input", "textarea", "select":
			fmt.Fprintf(&html, "%s<%s class=\"%s\" placeholder=%q></%s>\n", indent, tag, class, firstNonEmpty(n.AriaLabel, n.Text), tag)
			continue
		}
		fmt.Fprintf(&html, "%s<%s class=\"%s\">", indent, tag, class)
		if strings.TrimSpace(n.Text) != "" {
			html.WriteString(escapeHTML(n.Text))
		} else {
			html.WriteString("\n")
			open++
			continue
		}
		fmt.Fprintf(&html, "</%s>\n", tag)
	}
	for open > 1 {
		open--
		html.WriteString(strings.Repeat("  ", open) + "</div>\n")
	}
	html.WriteString("</section>\n")
	return html.String(), css.String()
}

func cssRule(s Styles) string {
	var parts []string
	if s.Display != "" && s.Display != "block" {
		parts = append(parts, "display: "+s.Display)
	}
	if s.FontSize > 0 {
		parts = append(parts, fmt.Sprintf("font-size: %spx", trimFloat(s.FontSize)))
	}
	if s.FontWeight != "" && s.FontWeight != "400" {
		parts = append(parts, "font-weight: "+s.FontWeight)
	}
	if s.LineHeight > 0 {
		parts = append(parts, fmt.Sprintf("line-height: %spx", trimFloat(s.LineHeight)))
	}
	if s.Color != "" {
		parts = append(parts, "color: "+s.Color)
	}
	if s.Background != "" {
		parts = append(parts, "background: "+s.Background)
	}
	for _, p := range []struct {
		name string
		v    float64
	}{{"padding-top", s.PadTop}, {"padding-bottom", s.PadBot}, {"padding-left", s.PadLeft}, {"padding-right", s.PadRght}, {"margin-top", s.MarginTop}, {"margin-bottom", s.MarginBot}} {
		if p.v > 0 {
			parts = append(parts, fmt.Sprintf("%s: %spx", p.name, trimFloat(p.v)))
		}
	}
	if s.Radius != "" {
		parts = append(parts, "border-radius: "+s.Radius)
	}
	if s.Shadow {
		parts = append(parts, "box-shadow: 0 1px 3px rgba(0,0,0,.12)")
	}
	return strings.Join(parts, "; ")
}

func cssTokens(t Tokens) string {
	var b strings.Builder
	b.WriteString(":root {\n")
	if len(t.FontSizes) > 0 {
		var parts []string
		for i, v := range t.FontSizes {
			if i >= 6 {
				break
			}
			parts = append(parts, fmt.Sprintf("--font-%d: %spx", i+1, trimFloat(v)))
		}
		b.WriteString("  " + strings.Join(parts, ";\n  ") + ";\n")
	}
	if len(t.Spacing) > 0 {
		var parts []string
		for i, v := range t.Spacing {
			if i >= 8 {
				break
			}
			parts = append(parts, fmt.Sprintf("--space-%d: %spx", i+1, trimFloat(v)))
		}
		b.WriteString("  " + strings.Join(parts, ";\n  ") + ";\n")
	}
	if len(t.Colors) > 0 {
		var parts []string
		for i, c := range t.Colors {
			if i >= 8 {
				break
			}
			parts = append(parts, fmt.Sprintf("--color-%d: %s", i+1, c))
		}
		b.WriteString("  " + strings.Join(parts, ";\n  ") + ";\n")
	}
	b.WriteString("}\n")
	return b.String()
}

func escapeHTML(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}
