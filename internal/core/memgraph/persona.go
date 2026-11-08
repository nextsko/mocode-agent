package memgraph

import (
	"fmt"
	"sort"
	"strings"
)

// BuildCard assembles the compact persona + user profile card from the graph.
func (s *Store) BuildCard() PersonaCard {
	s.mu.Lock()
	defer s.mu.Unlock()
	var card PersonaCard
	var persona, user *Node
	for _, n := range s.g.Nodes {
		if n.SupersededBy != "" {
			continue
		}
		switch n.Kind {
		case KindPersona:
			if persona == nil || n.UpdatedAt.After(persona.UpdatedAt) {
				nn := n
				persona = &nn
			}
		case KindUser:
			if user == nil || n.UpdatedAt.After(user.UpdatedAt) {
				nn := n
				user = &nn
			}
		}
	}
	if persona != nil {
		card.Persona = persona.Name
		card.Traits = propsList(persona.Props, "traits", "style", "values")
	}
	if user != nil {
		card.User = user.Name
	}
	// Preferences live on edges from user/persona to preference nodes.
	for _, e := range s.g.Edges {
		target, ok := s.g.Nodes[e.To]
		if !ok {
			continue
		}
		switch e.Kind {
		case EdgePrefers:
			card.Prefs = append(card.Prefs, target.Name)
		case EdgeKnows:
			if target.Kind == KindGoal {
				card.Goals = append(card.Goals, target.Name)
			}
		}
	}
	for _, n := range s.g.Nodes {
		if n.Kind == KindGoal && n.SupersededBy == "" {
			card.Goals = append(card.Goals, n.Name)
		}
	}
	for _, c := range s.ConflictsLocked() {
		card.Conflict = append(card.Conflict, c.Detail)
	}
	card.Prefs = dedupe(card.Prefs)
	card.Goals = dedupe(card.Goals)
	return card
}

// ConflictsLocked is the same as Conflicts but assumes the caller holds mu.
// It exists only for BuildCard; external callers use Conflicts.
func (s *Store) ConflictsLocked() []Conflict {
	return s.conflictsLocked()
}

// RenderCard renders the card as prompt-ready markdown.
func RenderCard(c PersonaCard) string {
	var b strings.Builder
	b.WriteString("## AI 人格\n\n")
	if c.Persona == "" {
		b.WriteString("_未定义（可从交互中沉淀）_\n")
	} else {
		fmt.Fprintf(&b, "- 角色: %s\n", c.Persona)
	}
	for _, t := range c.Traits {
		fmt.Fprintf(&b, "- 特质: %s\n", t)
	}
	b.WriteString("\n## 用户画像\n\n")
	if c.User != "" {
		fmt.Fprintf(&b, "- 用户: %s\n", c.User)
	}
	if len(c.Prefs) > 0 {
		b.WriteString("\n### 偏好\n\n")
		for _, p := range c.Prefs {
			fmt.Fprintf(&b, "- %s\n", p)
		}
	}
	if len(c.Goals) > 0 {
		b.WriteString("\n### 目标\n\n")
		for _, g := range c.Goals {
			fmt.Fprintf(&b, "- %s\n", g)
		}
	}
	if len(c.Conflict) > 0 {
		b.WriteString("\n### 待澄清\n\n")
		for _, x := range c.Conflict {
			fmt.Fprintf(&b, "- %s\n", x)
		}
	}
	return b.String()
}

// PromptCard returns a budgeted card for context injection.
func PromptCard(c PersonaCard, maxChars int) string {
	text := RenderCard(c)
	if maxChars > 0 && len(text) > maxChars {
		text = text[:maxChars] + "…"
	}
	return text
}

func propsList(props map[string]any, keys ...string) []string {
	var out []string
	for _, k := range keys {
		v, ok := props[k]
		if !ok {
			continue
		}
		switch t := v.(type) {
		case string:
			out = append(out, t)
		case []string:
			out = append(out, t...)
		case []any:
			for _, x := range t {
				if s, ok := x.(string); ok {
					out = append(out, s)
				}
			}
		}
	}
	return dedupe(out)
}

func dedupe(in []string) []string {
	set := map[string]bool{}
	var out []string
	for _, v := range in {
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
