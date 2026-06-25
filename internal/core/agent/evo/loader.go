package evo

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// AgentEntry is a tamed evo agent ready to be surfaced as a selectable mode.
// It pairs the manifest (identity) with the active revision's固化 prompt.
type AgentEntry struct {
	Manifest Manifest
	Revision Revision
}

// LoadAgents enumerates every fixed evo agent under root and returns the ones
// whose active revision is loadable, sorted by name. Agents with a missing or
// unreadable active revision are skipped. This is the read path that lets the
// mode picker list tamed agents.
func LoadAgents(ctx context.Context, root string) ([]AgentEntry, error) {
	s := NewFixationStore(root)
	manifests, err := s.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("evo: list agents: %w", err)
	}
	var out []AgentEntry
	for _, m := range manifests {
		rev, err := s.readRevisionLocked(m.Name, m.ActiveRevision)
		if err != nil {
			continue
		}
		out = append(out, AgentEntry{Manifest: m, Revision: rev})
	}
	return out, nil
}

// LoadAgent returns a single tamed agent by name. Returns an error wrapping
// os.ErrNotExist when the agent has never been fixed.
func LoadAgent(ctx context.Context, root, name string) (AgentEntry, error) {
	s := NewFixationStore(root)
	m, rev, err := s.Load(ctx, name)
	if err != nil {
		return AgentEntry{}, err
	}
	return AgentEntry{Manifest: m, Revision: rev}, nil
}

// PromptFile returns the path a loader would write a固化 prompt to if it
// materialized the agent as an on-disk mode file. Used by exporters.
func PromptFile(root, name string) string {
	return filepath.Join(AgentDir(root, name), "system_prompt.md")
}

// ModeID derives a stable, namespaced mode id for a tamed agent so it does
// not collide with built-in agent ids (e.g. "coder"): "evo:<name>".
func ModeID(name string) string {
	return "evo:" + sanitizeName(name)
}

// Summary renders a one-line description for a mode picker entry.
func (a AgentEntry) Summary() string {
	if a.Manifest.Description != "" {
		return a.Manifest.Description
	}
	return fmt.Sprintf("tamed evo agent (%d iterations)", a.Manifest.Iterations)
}

// SkillList renders the captured skills as a comma-separated string for
// display; empty when no skills were captured.
func (a AgentEntry) SkillList() string {
	if len(a.Revision.Skills) == 0 {
		return ""
	}
	return strings.Join(a.Revision.Skills, ", ")
}