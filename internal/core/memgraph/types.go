// Package memgraph is a traceable, structured memory graph for an agent.
//
// The design is deliberately graph-first: every belief, preference, decision
// and persona trait is a node; relationships are edges; every node and edge
// carries provenance (who said it, where, when) and a confidence score. That
// makes memory explainable ("why do you think I prefer X?") and revisable
// (supersede instead of overwrite), which is what a long-lived agent needs.
package memgraph

import "time"

// Node kinds.
const (
	KindUser       = "user"
	KindPersona    = "persona"
	KindPreference = "preference"
	KindFact       = "fact"
	KindGoal       = "goal"
	KindDecision   = "decision"
	KindTask       = "task"
	KindSkill      = "skill"
	KindProject    = "project"
	KindEvent      = "event"
	KindArtifact   = "artifact"
)

// Edge kinds.
const (
	EdgePrefers     = "prefers"
	EdgeKnows       = "knows"
	EdgeDecided     = "decided"
	EdgeProduced    = "produced"
	EdgeDerivedFrom = "derived_from"
	EdgeSupersedes  = "supersedes"
	EdgeContradicts = "contradicts"
	EdgeRelatesTo   = "relates_to"
	EdgePartOf      = "part_of"
	EdgeEvidencedBy = "evidenced_by"
)

// Source is one piece of evidence behind a node/edge.
type Source struct {
	Kind  string    `json:"kind"` // session | message | task | url | user | model
	Ref   string    `json:"ref,omitempty"`
	Quote string    `json:"quote,omitempty"`
	At    time.Time `json:"at"`
}

// Node is one entity in the memory graph.
type Node struct {
	ID         string         `json:"id"`
	Kind       string         `json:"kind"`
	Name       string         `json:"name"`
	Props      map[string]any `json:"props,omitempty"`
	Confidence float64        `json:"confidence"`
	Tags       []string       `json:"tags,omitempty"`
	Sources    []Source       `json:"sources,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
	// ValidFrom/ValidTo make beliefs temporal: an agent can remember that a
	// preference USED to hold without treating it as current.
	ValidFrom *time.Time `json:"valid_from,omitempty"`
	ValidTo   *time.Time `json:"valid_to,omitempty"`
	// SupersededBy points at the node that replaced this belief.
	SupersededBy string `json:"superseded_by,omitempty"`
}

// Edge is a relationship between two nodes.
type Edge struct {
	ID         string    `json:"id"`
	From       string    `json:"from"`
	To         string    `json:"to"`
	Kind       string    `json:"kind"`
	Weight     float64   `json:"weight"`
	Confidence float64   `json:"confidence"`
	Sources    []Source  `json:"sources,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// Graph is the full snapshot.
type Graph struct {
	Version   int             `json:"version"`
	Nodes     map[string]Node `json:"nodes"`
	Edges     map[string]Edge `json:"edges"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// EvidenceChain is one explanation path: start node → … → source.
type EvidenceChain struct {
	Path       []string `json:"path"`
	EdgeKinds  []string `json:"edge_kinds,omitempty"`
	Sources    []Source `json:"sources,omitempty"`
	Confidence float64  `json:"confidence"`
}

// PersonaCard is the compact, prompt-ready summary of who the agent is and
// who the user is.
type PersonaCard struct {
	Persona  string   `json:"persona,omitempty"`
	User     string   `json:"user,omitempty"`
	Traits   []string `json:"traits,omitempty"`
	Prefs    []string `json:"preferences,omitempty"`
	Goals    []string `json:"goals,omitempty"`
	Conflict []string `json:"conflicts,omitempty"`
}

// Event is one append-only mutation record; replaying it reconstructs history.
type Event struct {
	At   time.Time `json:"at"`
	Op   string    `json:"op"` // upsert_node | upsert_edge | forget | supersede
	Node *Node     `json:"node,omitempty"`
	Edge *Edge     `json:"edge,omitempty"`
	ID   string    `json:"id,omitempty"`
}
