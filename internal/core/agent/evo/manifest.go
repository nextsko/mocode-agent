package evo

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// Manifest describes a fixed (固化) evolution agent: the product of a /evo
// session that has been snapshotted for later reuse as a mode-like assistant.
// It is persisted as agent.json next to the agent's context artifacts.
type Manifest struct {
	// Name is the unique, filesystem-safe agent identifier (e.g. "reviewer").
	// It forms the directory name under the evo root: agent-<Name>.
	Name string `json:"name"`

	// Title is the human-readable label shown in the mode picker.
	Title string `json:"title"`

	// Description summarizes what this agent was tamed to do well.
	Description string `json:"description,omitempty"`

	// SystemPrompt is the固化 system prompt — the optimal theory the agent
	// converged on. Loaded verbatim as the agent's base prompt.
	SystemPrompt string `json:"system_prompt"`

	// Skills lists the emergent skill IDs captured during evolution, by name.
	// The loader resolves each to its SKILL.md at load time.
	Skills []string `json:"skills,omitempty"`

	// ActiveRevision is the revision ID currently promoted as the live agent.
	// Older revisions remain archived for rollback.
	ActiveRevision string `json:"active_revision"`

	// CreatedAt is when the agent was first fixed.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the active revision last changed.
	UpdatedAt time.Time `json:"updated_at"`

	// Iterations counts how many /evo runs contributed to this agent.
	Iterations int `json:"iterations"`
}

// DirName returns the on-disk directory name for this agent: "agent-<Name>".
func (m Manifest) DirName() string {
	return "agent-" + sanitizeName(m.Name)
}

// sanitizeName lowercases, drops everything but [a-z0-9_-], and collapses
// runs so the name is safe as a path segment.
func sanitizeName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	prevDash := false
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_' || r == '-':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteRune('-')
				prevDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "agent"
	}
	return out
}

// AgentDir returns the absolute directory for an agent under the given evo
// root: <root>/agent-<name>.
func AgentDir(root, name string) string {
	return filepath.Join(root, "agent-"+sanitizeName(name))
}

// ManifestPath returns the path to an agent's manifest file.
func ManifestPath(root, name string) string {
	return filepath.Join(AgentDir(root, name), "agent.json")
}

// Validate checks the manifest has the required fields for loading.
func (m Manifest) Validate() error {
	if strings.TrimSpace(m.Name) == "" {
		return fmt.Errorf("evo manifest: name is required")
	}
	if strings.TrimSpace(m.SystemPrompt) == "" {
		return fmt.Errorf("evo manifest: system_prompt is required")
	}
	if strings.TrimSpace(m.ActiveRevision) == "" {
		return fmt.Errorf("evo manifest: active_revision is required")
	}
	return nil
}
