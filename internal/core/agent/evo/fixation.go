package evo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// RevisionStatus tracks a fixation revision's lifecycle.
type RevisionStatus string

const (
	// RevisionActive is the currently-promoted, live revision.
	RevisionActive RevisionStatus = "active"
	// RevisionArchived is a superseded revision kept for rollback.
	RevisionArchived RevisionStatus = "archived"
)

// Revision is one fixed snapshot of an agent's optimal context. Revisions
// are immutable once written; promotion flips status between active/archived.
type Revision struct {
	// ID is a unique, sortable revision identifier (timestamp-based).
	ID string `json:"id"`
	// AgentName matches the parent manifest Name.
	AgentName string `json:"agent_name"`
	// Status is active or archived.
	Status RevisionStatus `json:"status"`
	// SystemPrompt is the固化 prompt snapshotted at this revision.
	SystemPrompt string `json:"system_prompt"`
	// Skills are the emergent skill names captured at this revision.
	Skills []string `json:"skills,omitempty"`
	// Reason explains why this revision was fixed (e.g. "iter 3, score 0.82").
	Reason string `json:"reason,omitempty"`
	// CreatedAt is when the revision was written.
	CreatedAt time.Time `json:"created_at"`
}

// FixationStore persists revisioned agent snapshots under an evo root. It is
// the "seed-fixation" mechanism: a successful /evo run fixes the agent's
// converged context as a new revision, the prior active revision is archived,
// and the active pointer moves forward. Older revisions stay available for
// rollback. All operations are file-based (JSON) and safe for concurrent use
// within a process.
//
// Layout under <root>:
//
//	agent-<name>/
//	  agent.json          # Manifest (name, title, active_revision, ...)
//	  revisions/
//	    <id>.json         # one Revision file per fixation
type FixationStore struct {
	mu   sync.Mutex
	root string
}

// NewFixationStore returns a store rooted at the given evo directory. The
// directory is created on first write, not at construction.
func NewFixationStore(root string) *FixationStore {
	return &FixationStore{root: root}
}

// Root returns the evo root directory.
func (s *FixationStore) Root() string { return s.root }

// Fix promotes a new revision for the named agent and archives the previous
// active one. If the agent has no manifest yet, one is created from the
// revision. The returned Revision is the newly-active one. It is an error to
// fix a revision with an empty system prompt.
func (s *FixationStore) Fix(ctx context.Context, agentName, title, description string, rev Revision) (*Revision, error) {
	if strings.TrimSpace(rev.SystemPrompt) == "" {
		return nil, errors.New("evo: cannot fix a revision with an empty system prompt")
	}
	if strings.TrimSpace(agentName) == "" {
		return nil, errors.New("evo: agent name is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	if rev.ID == "" {
		rev.ID = revisionID(now)
	}
	rev.AgentName = agentName
	rev.Status = RevisionActive
	rev.CreatedAt = now

	agentDir := AgentDir(s.root, agentName)
	revisionsDir := filepath.Join(agentDir, "revisions")
	if err := os.MkdirAll(revisionsDir, 0o755); err != nil {
		return nil, fmt.Errorf("evo: create revisions dir: %w", err)
	}

	// Load existing manifest (if any) to archive its active revision.
	manifest, _ := s.readManifestLocked(agentName)
	if manifest != nil && manifest.ActiveRevision != "" && manifest.ActiveRevision != rev.ID {
		if err := s.archiveLocked(agentName, manifest.ActiveRevision); err != nil {
			return nil, fmt.Errorf("evo: archive prior revision: %w", err)
		}
	}

	// Write the new revision file.
	if err := s.writeRevisionLocked(agentName, rev); err != nil {
		return nil, fmt.Errorf("evo: write revision: %w", err)
	}

	// Upsert the manifest pointing at the new active revision.
	if manifest == nil {
		manifest = &Manifest{
			Name:      agentName,
			Title:     title,
			CreatedAt: now,
		}
	}
	if title != "" {
		manifest.Title = title
	}
	if description != "" {
		manifest.Description = description
	}
	manifest.SystemPrompt = rev.SystemPrompt
	manifest.Skills = rev.Skills
	manifest.ActiveRevision = rev.ID
	manifest.UpdatedAt = now
	manifest.Iterations++

	if err := s.writeManifestLocked(agentName, *manifest); err != nil {
		return nil, fmt.Errorf("evo: write manifest: %w", err)
	}
	return &rev, nil
}

// Load returns the active revision and manifest for a named agent. Returns
// os.ErrNotExist if the agent has never been fixed.
func (s *FixationStore) Load(ctx context.Context, agentName string) (Manifest, Revision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	manifest, err := s.readManifestLocked(agentName)
	if err != nil {
		return Manifest{}, Revision{}, err
	}
	rev, err := s.readRevisionLocked(agentName, manifest.ActiveRevision)
	if err != nil {
		return Manifest{}, Revision{}, err
	}
	return *manifest, rev, nil
}

// List returns the manifests of every fixed agent, sorted by name.
func (s *FixationStore) List(ctx context.Context) ([]Manifest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []Manifest
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "agent-") {
			continue
		}
		agentName := strings.TrimPrefix(e.Name(), "agent-")
		m, err := s.readManifestLocked(agentName)
		if err != nil {
			continue // skip unreadable agents
		}
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// History returns all revisions for an agent, newest first.
func (s *FixationStore) History(ctx context.Context, agentName string) ([]Revision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := filepath.Join(AgentDir(s.root, agentName), "revisions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []Revision
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		rev, err := s.readRevisionLocked(agentName, strings.TrimSuffix(e.Name(), ".json"))
		if err != nil {
			continue
		}
		out = append(out, rev)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// Rollback re-points the active revision to a prior archived revision. The
// current active revision is archived first.
func (s *FixationStore) Rollback(ctx context.Context, agentName, targetRevisionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	manifest, err := s.readManifestLocked(agentName)
	if err != nil {
		return err
	}
	target, err := s.readRevisionLocked(agentName, targetRevisionID)
	if err != nil {
		return fmt.Errorf("evo: rollback target not found: %w", err)
	}
	if manifest.ActiveRevision != "" && manifest.ActiveRevision != targetRevisionID {
		if err := s.archiveLocked(agentName, manifest.ActiveRevision); err != nil {
			return err
		}
	}
	target.Status = RevisionActive
	if err := s.writeRevisionLocked(agentName, target); err != nil {
		return err
	}
	manifest.ActiveRevision = target.ID
	manifest.SystemPrompt = target.SystemPrompt
	manifest.Skills = target.Skills
	manifest.UpdatedAt = time.Now().UTC()
	return s.writeManifestLocked(agentName, *manifest)
}

// --- locked helpers (caller holds s.mu) ---

func (s *FixationStore) archiveLocked(agentName, revisionID string) error {
	rev, err := s.readRevisionLocked(agentName, revisionID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if rev.Status == RevisionArchived {
		return nil
	}
	rev.Status = RevisionArchived
	return s.writeRevisionLocked(agentName, rev)
}

func (s *FixationStore) writeRevisionLocked(agentName string, rev Revision) error {
	dir := filepath.Join(AgentDir(s.root, agentName), "revisions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(rev, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, rev.ID+".json"), data, 0o644)
}

func (s *FixationStore) readRevisionLocked(agentName, revisionID string) (Revision, error) {
	var rev Revision
	data, err := os.ReadFile(filepath.Join(AgentDir(s.root, agentName), "revisions", revisionID+".json"))
	if err != nil {
		return rev, err
	}
	return rev, json.Unmarshal(data, &rev)
}

func (s *FixationStore) writeManifestLocked(agentName string, m Manifest) error {
	dir := AgentDir(s.root, agentName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "agent.json"), data, 0o644)
}

func (s *FixationStore) readManifestLocked(agentName string) (*Manifest, error) {
	data, err := os.ReadFile(ManifestPath(s.root, agentName))
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// revisionID builds a sortable, unique revision ID from a timestamp.
func revisionID(t time.Time) string {
	return t.Format("20060102-150405.000")
}
