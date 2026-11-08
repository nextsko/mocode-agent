package agenttools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nextsko/mocode-agent/internal/core/memgraph"
	"github.com/nextsko/mocode-agent/internal/core/tools/contract"
)

//go:embed memgraph.md
var memgraphDescription []byte

// MemgraphToolName is the registered name of the memory graph tool.
const MemgraphToolName = "memgraph"

// MemgraphParams controls graph memory operations.
type MemgraphParams struct {
	Action       string         `json:"action" enum:"remember,relate,recall,explain,persona,conflicts,forget,export,graph,decay" description:"Graph memory operations."`
	Kind         string         `json:"kind,omitempty" enum:"user,persona,preference,fact,goal,decision,task,skill,project,event,artifact" description:"Node kind for remember."`
	Name         string         `json:"name,omitempty" description:"Node name for remember/search."`
	Props        map[string]any `json:"props,omitempty" description:"Arbitrary structured fields (role, style, value, ...)."`
	Confidence   float64        `json:"confidence,omitempty" description:"0..1 belief confidence (default 0.6)."`
	ID           string         `json:"id,omitempty" description:"Node id for explain/forget/recall neighbourhood."`
	From         string         `json:"from,omitempty" description:"Edge source node id."`
	To           string         `json:"to,omitempty" description:"Edge target node id."`
	EdgeKind     string         `json:"edge_kind,omitempty" description:"prefers, knows, decided, produced, derived_from, supersedes, contradicts, relates_to, part_of, evidenced_by."`
	Depth        int            `json:"depth,omitempty" description:"Neighbourhood depth for recall (default 1)."`
	Query        string         `json:"query,omitempty" description:"Case-insensitive search for recall."`
	SourceKind   string         `json:"source_kind,omitempty" description:"Evidence kind: session/message/task/url/user/model."`
	SourceRef    string         `json:"source_ref,omitempty" description:"Evidence reference (session or message id, URL)."`
	SourceQuote  string         `json:"source_quote,omitempty" description:"Short quote proving the belief."`
	HalfLifeDays int            `json:"half_life_days,omitempty" description:"For decay: half-life in days (default 30)."`
	Floor        float64        `json:"floor,omitempty" description:"For decay: minimum confidence (default 0.1)."`
}

// NewMemgraphPortTool builds the graph memory tool for one project dir.
func NewMemgraphPortTool(workingDir string) contract.Tool {
	return contract.NewFunc(
		MemgraphToolName,
		"Traceable graph memory: personas, user profile, preferences, decisions and evidence.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				schemaActionKey: map[string]any{
					"type":        "string",
					"enum":        []string{"remember", "relate", "recall", "explain", "persona", "conflicts", "forget", "export", "graph", "decay"},
					"description": "Graph memory operations.",
				},
				"kind":           map[string]any{"type": "string", "enum": []string{"user", "persona", "preference", "fact", "goal", "decision", "task", "skill", "project", "event", "artifact"}, "description": "Node kind for remember."},
				"name":           map[string]any{"type": "string", "description": "Node name for remember/search."},
				"props":          map[string]any{"type": "object", "description": "Arbitrary structured fields (role, style, value, ...)."},
				"confidence":     map[string]any{"type": "number", "description": "0..1 belief confidence (default 0.6)."},
				"id":             map[string]any{"type": "string", "description": "Node id for explain/forget/recall neighbourhood."},
				"from":           map[string]any{"type": "string", "description": "Edge source node id."},
				"to":             map[string]any{"type": "string", "description": "Edge target node id."},
				"edge_kind":      map[string]any{"type": "string", "description": "prefers, knows, decided, produced, derived_from, supersedes, contradicts, relates_to, part_of, evidenced_by."},
				"depth":          map[string]any{"type": "integer", "description": "Neighbourhood depth for recall (default 1)."},
				"query":          map[string]any{"type": "string", "description": "Case-insensitive search for recall."},
				"source_kind":    map[string]any{"type": "string", "description": "Evidence kind: session/message/task/url/user/model."},
				"source_ref":     map[string]any{"type": "string", "description": "Evidence reference (session or message id, URL)."},
				"source_quote":   map[string]any{"type": "string", "description": "Short quote proving the belief."},
				"half_life_days": map[string]any{"type": "integer", "description": "For decay: half-life in days (default 30)."},
				"floor":          map[string]any{"type": "number", "description": "For decay: minimum confidence (default 0.1)."},
			},
			"required": []string{schemaActionKey},
		},
		func(_ context.Context, _ contract.ToolContext, args json.RawMessage) (contract.ToolResult, error) {
			var p MemgraphParams
			if len(args) > 0 {
				if err := json.Unmarshal(args, &p); err != nil {
					return contract.ToolResult{}, fmt.Errorf("invalid parameters: %w", err)
				}
			}
			return runMemgraph(workingDir, p)
		},
	)
}

func runMemgraph(workingDir string, p MemgraphParams) (contract.ToolResult, error) {
	s, err := memgraph.Open(workingDir)
	if err != nil {
		return contract.Error("failed to open memory graph: " + err.Error()), nil //nolint:nilerr // tool-level error is reported as text to the model.
	}

	switch strings.ToLower(strings.TrimSpace(p.Action)) {
	case "remember":
		if p.Name == "" {
			return contract.Error("name is required for action=remember"), nil
		}
		n := memgraph.Node{
			Kind: p.Kind, Name: p.Name, Props: p.Props, Confidence: p.Confidence,
		}
		if p.SourceQuote != "" || p.SourceRef != "" {
			n.Sources = []memgraph.Source{{
				Kind: firstNonEmptyStr(p.SourceKind, "model"), Ref: p.SourceRef,
				Quote: p.SourceQuote, At: time.Now().UTC(),
			}}
		}
		got, err := s.UpsertNode(n)
		if err != nil {
			return contract.Error("failed to remember: " + err.Error()), nil //nolint:nilerr // tool-level error is reported as text to the model.
		}
		return contract.Text(fmt.Sprintf("Remembered %s %q as %s (confidence %.2f)", got.Kind, got.Name, got.ID, got.Confidence)), nil

	case "relate":
		if p.From == "" || p.To == "" || p.EdgeKind == "" {
			return contract.Error("from, to and edge_kind are required for action=relate"), nil
		}
		e, err := s.UpsertEdge(memgraph.Edge{From: p.From, To: p.To, Kind: p.EdgeKind, Confidence: p.Confidence})
		if err != nil {
			return contract.Error("failed to relate: " + err.Error()), nil //nolint:nilerr // tool-level error is reported as text to the model.
		}
		return contract.Text(fmt.Sprintf("Related %s -[%s]-> %s", e.From, e.Kind, e.To)), nil

	case "recall":
		if p.ID != "" {
			nodes, edges := s.Neighbors(p.ID, p.Depth)
			var b strings.Builder
			fmt.Fprintf(&b, "Neighbourhood of %s (%d nodes, %d edges):\n", p.ID, len(nodes), len(edges))
			for _, n := range nodes {
				fmt.Fprintf(&b, "- [%s] %s (%s, conf %.2f)\n", n.Kind, n.Name, n.ID, n.Confidence)
			}
			for _, e := range edges {
				fmt.Fprintf(&b, "- %s -[%s]-> %s\n", e.From, e.Kind, e.To)
			}
			return contract.Text(b.String()), nil
		}
		nodes := s.Search(p.Kind, p.Query)
		if len(nodes) == 0 {
			return contract.Text("No matching memory nodes."), nil
		}
		var b strings.Builder
		for i, n := range nodes {
			if i >= 50 {
				break
			}
			fmt.Fprintf(&b, "- [%s] %s (%s, conf %.2f, updated %s)\n", n.Kind, n.Name, n.ID, n.Confidence, n.UpdatedAt.Format("2006-01-02"))
		}
		return contract.Text(b.String()), nil

	case "explain":
		if p.ID == "" {
			return contract.Error("id is required for action=explain"), nil
		}
		n, ok := s.Get(p.ID)
		if !ok {
			return contract.Error("node not found: " + p.ID), nil
		}
		return contract.Text(memgraph.RenderExplain(n.Name, s.Explain(p.ID))), nil

	case "persona":
		card := s.BuildCard()
		return contract.Text(memgraph.RenderCard(card)), nil

	case "conflicts":
		conflicts := s.Conflicts()
		if len(conflicts) == 0 {
			return contract.Text("No conflicts — the graph is consistent."), nil
		}
		var b strings.Builder
		for _, c := range conflicts {
			fmt.Fprintf(&b, "- %s (resolve with edge_kind=supersedes)\n", c.Detail)
		}
		return contract.Text(b.String()), nil

	case "forget":
		if p.ID == "" {
			return contract.Error("id is required for action=forget"), nil
		}
		removed, err := s.Forget(p.ID)
		if err != nil {
			return contract.Error(err.Error()), nil
		}
		return contract.Text(fmt.Sprintf("Forgot %s (%d records removed)", p.ID, removed)), nil

	case "export":
		dir, err := s.ExportBundle(workingDir)
		if err != nil {
			return contract.Error("failed to export: " + err.Error()), nil //nolint:nilerr // tool-level error is reported as text to the model.
		}
		return contract.Text("Memory graph exported: `" + dir + "` (graph.json, graph.mmd, graph.html)"), nil

	case "graph":
		return contract.Text(memgraph.RenderMermaid(s.Graph())), nil

	case "decay":
		half := time.Duration(p.HalfLifeDays) * 24 * time.Hour
		if half <= 0 {
			half = 30 * 24 * time.Hour
		}
		floor := p.Floor
		if floor <= 0 {
			floor = 0.1
		}
		changed, err := s.Decay(half, floor)
		if err != nil {
			return contract.Error("failed to decay: " + err.Error()), nil //nolint:nilerr // tool-level error is reported as text to the model.
		}
		return contract.Text(fmt.Sprintf("Decayed %d nodes (half-life %s, floor %.2f).", changed, half, floor)), nil

	default:
		return contract.Error("action must be one of: remember, relate, recall, explain, persona, conflicts, forget, export, graph, decay"), nil
	}
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
