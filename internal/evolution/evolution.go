// Package evolution provides the self-evolving agent patch system.
//
// Patches are the output of the evolution agent: after analyzing session
// logs, it produces structured patches that strengthen the system over time.
// Each patch is a directory under .mocode/patches/<patch-id>/ containing:
//
//	patch.json   - metadata (id, timestamp, sessions analyzed, priority)
//	skills/      - new SKILL.md files the agent can use
//	memory/      - facts to persist for future retrieval
//	rules/       - prompt constraint additions (injected into system prompt)
//	plans/       - improved process templates
//	info/        - knowledge entries
//	export/      - shareable artifacts
package evolution

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ── Patch ───────────────────────────────────────────────────────────────────

// PatchKind classifies the type of improvement.
type PatchKind string

const (
	KindRule   PatchKind = "rule"   // prompt constraint addition
	KindMemory PatchKind = "memory" // fact to remember
	KindSkill  PatchKind = "skill"  // new agent skill
	KindPlan   PatchKind = "plan"   // process template improvement
	KindInfo   PatchKind = "info"   // knowledge entry
	KindExport PatchKind = "export" // shareable artifact
)

// Patch metadata stored in patch.json.
type Patch struct {
	ID          string    `json:"id"`
	Kind        PatchKind `json:"kind"`
	Title       string    `json:"title"`       // one-line summary
	Description string    `json:"description"` // what problem it solves
	Priority    int       `json:"priority"`    // 1=critical, 5=low
	Source      string    `json:"source"`      // session ID(s) that triggered this
	CreatedAt   time.Time `json:"created_at"`
	Applied     bool      `json:"applied"` // has been loaded into context
	AppliedAt   time.Time `json:"applied_at,omitempty"`

	// InjectCount tracks how many times this patch has been injected into a
	// system prompt. When it reaches MaxInjects the patch is auto-applied
	// (convergence), preventing unbounded context growth.
	InjectCount int `json:"inject_count,omitempty"`
	// MaxInjects caps how many sessions a patch is injected into before it is
	// considered learned and auto-applied. Zero means no auto-convergence
	// (inject until manually applied).
	MaxInjects int `json:"max_injects,omitempty"`

	// File paths relative to the patch directory.
	Files []string `json:"files,omitempty"`
}

// ── PatchStore ──────────────────────────────────────────────────────────────

// PatchStore manages the patch directory.
type PatchStore struct {
	dir string // .mocode/patches/
}

// NewPatchStore creates a store rooted at the given directory.
func NewPatchStore(baseDir string) (*PatchStore, error) {
	dir := filepath.Join(baseDir, "patches")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create patches dir: %w", err)
	}
	return &PatchStore{dir: dir}, nil
}

// CreatePatch creates a new patch directory and writes patch.json.
func (s *PatchStore) CreatePatch(p Patch) (string, error) {
	if p.ID == "" {
		p.ID = "patch-" + time.Now().Format("20060102-150405")
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now()
	}

	patchDir := filepath.Join(s.dir, p.ID)

	// Create required subdirectories.
	for _, sub := range []string{"skills", "memory", "rules", "plans", "info", "export"} {
		if err := os.MkdirAll(filepath.Join(patchDir, sub), 0o755); err != nil {
			return "", fmt.Errorf("create subdir %s: %w", sub, err)
		}
	}

	// Write patch.json.
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal patch: %w", err)
	}
	metaPath := filepath.Join(patchDir, "patch.json")
	if err := os.WriteFile(metaPath, data, 0o644); err != nil {
		return "", fmt.Errorf("write patch.json: %w", err)
	}

	return patchDir, nil
}

// WriteFile writes content to a file within a patch directory.
func (s *PatchStore) WriteFile(patchDir, subDir, filename, content string) error {
	targetDir := filepath.Join(patchDir, subDir)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(targetDir, filename), []byte(content), 0o644)
}

// List returns all patches sorted by creation time (newest first).
func (s *PatchStore) List() ([]Patch, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var patches []Patch
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		metaPath := filepath.Join(s.dir, e.Name(), "patch.json")
		data, err := os.ReadFile(metaPath)
		if err != nil {
			continue
		}
		var p Patch
		if err := json.Unmarshal(data, &p); err != nil {
			continue
		}
		patches = append(patches, p)
	}

	sort.Slice(patches, func(i, j int) bool {
		return patches[i].CreatedAt.After(patches[j].CreatedAt)
	})
	return patches, nil
}

// ListUnapplied returns patches that haven't been applied yet, by priority.
func (s *PatchStore) ListUnapplied() ([]Patch, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}
	var unapplied []Patch
	for _, p := range all {
		if !p.Applied {
			unapplied = append(unapplied, p)
		}
	}
	sort.Slice(unapplied, func(i, j int) bool {
		return unapplied[i].Priority < unapplied[j].Priority // lower = more urgent
	})
	return unapplied, nil
}

// MarkApplied sets a patch as applied.
func (s *PatchStore) MarkApplied(patchID string) error {
	metaPath := filepath.Join(s.dir, patchID, "patch.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return err
	}
	var p Patch
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	p.Applied = true
	p.AppliedAt = time.Now()
	data, err = json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath, data, 0o644)
}

// MarkInjected increments a patch's InjectCount and, if MaxInjects > 0 and the
// count reaches it, marks the patch Applied (convergence). This is the
// auto-convergence mechanism that prevents unapplied patches from accumulating
// in every system prompt forever.
func (s *PatchStore) MarkInjected(patchID string) error {
	metaPath := filepath.Join(s.dir, patchID, "patch.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return err
	}
	var p Patch
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	p.InjectCount++
	if p.MaxInjects > 0 && p.InjectCount >= p.MaxInjects {
		p.Applied = true
		p.AppliedAt = time.Now()
	}
	data, err = json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath, data, 0o644)
}

// Dir returns the patches root directory.
func (s *PatchStore) Dir() string { return s.dir }

// ── PatchLoader ─────────────────────────────────────────────────────────────

// PatchLoader loads unapplied patches and compiles context for the agent.
type PatchLoader struct {
	store *PatchStore
}

// NewPatchLoader creates a loader backed by the given store.
func NewPatchLoader(store *PatchStore) *PatchLoader {
	return &PatchLoader{store: store}
}

// BuildContext reads all unapplied patches and returns a Markdown string
// suitable for appending to the system prompt as <evolution_context>.
// It also reads patch file contents (rules, info, etc.).
func (l *PatchLoader) BuildContext() (string, error) {
	patches, err := l.store.ListUnapplied()
	if err != nil {
		return "", err
	}
	if len(patches) == 0 {
		return "", nil
	}

	var sb strings.Builder
	sb.WriteString("## Self-Evolution Patches (Learned from Past Sessions)\n\n")
	sb.WriteString("The following improvements were discovered by the evolution agent. ")
	sb.WriteString("Apply these lessons during this session.\n\n")

	ruleCount := 0
	for _, p := range patches {
		sb.WriteString(fmt.Sprintf("### %s [%s] (priority: %d)\n", p.Title, p.Kind, p.Priority))
		sb.WriteString(fmt.Sprintf("%s\n\n", p.Description))

		// Convergence: count this injection. Patches whose InjectCount reaches
		// MaxInjects are auto-applied by MarkInjected, so they drop out of the
		// unapplied set on subsequent runs instead of accumulating forever.
		if p.MaxInjects > 0 {
			_ = l.store.MarkInjected(p.ID)
		}

		// Load rule files — these are injected directly as constraints.
		if p.Kind == KindRule {
			rulesDir := filepath.Join(l.store.Dir(), p.ID, "rules")
			entries, err := os.ReadDir(rulesDir)
			if err == nil {
				for _, e := range entries {
					if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
						continue
					}
					data, _ := os.ReadFile(filepath.Join(rulesDir, e.Name()))
					if len(data) > 0 {
						sb.WriteString(string(data))
						sb.WriteString("\n\n")
						ruleCount++
					}
				}
			}
		}
	}
	if ruleCount > 0 {
		sb.WriteString(fmt.Sprintf("\n> %d rule patch(es) loaded into context.\n\n", ruleCount))
	}
	return sb.String(), nil
}

// ApplyAll marks all currently unapplied patches as applied.
func (l *PatchLoader) ApplyAll() error {
	patches, err := l.store.ListUnapplied()
	if err != nil {
		return err
	}
	for _, p := range patches {
		if err := l.store.MarkApplied(p.ID); err != nil {
			return err
		}
	}
	return nil
}
