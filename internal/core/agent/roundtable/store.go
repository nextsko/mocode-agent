package roundtable

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Snapshot is the on-disk representation of a Roundtable. It can be saved
// and loaded to resume a meeting across process restarts.
type Snapshot struct {
	Config       Config      `json:"config"`
	Phase        Phase       `json:"phase"`
	Transcript   []Statement `json:"transcript"`
	CurrentSeq   int         `json:"current_seq"`
	CurrentTurn  int         `json:"current_turn"`
	Usage        Usage       `json:"usage"`
	Consensus    *Consensus  `json:"consensus,omitempty"`
	MaxTurnsHit  bool        `json:"max_turns_hit"`
	LoopDetected bool        `json:"loop_detected"`
	SavedAt      int64       `json:"saved_at"`
}

// Store persists roundtable snapshots to the local filesystem.
type Store struct {
	baseDir string
}

// NewStore creates a roundtable store rooted at baseDir.
func NewStore(baseDir string) *Store {
	return &Store{baseDir: baseDir}
}

func (s *Store) roundtablesDir() string {
	return filepath.Join(s.baseDir, "roundtables")
}

func (s *Store) snapshotPath(id string) string {
	return filepath.Join(s.roundtablesDir(), id+".json")
}

// Save writes the current roundtable state to disk.
func (s *Store) Save(ctx context.Context, rt *Roundtable) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	snap := Snapshot{
		Config:       rt.Config,
		Phase:        rt.Phase,
		Transcript:   rt.Transcript,
		CurrentSeq:   rt.CurrentSeq,
		CurrentTurn:  rt.CurrentTurn,
		Usage:        rt.Usage,
		Consensus:    rt.Consensus,
		MaxTurnsHit:  rt.MaxTurnsHit,
		LoopDetected: rt.LoopDetected,
		SavedAt:      time.Now().Unix(),
	}

	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal roundtable snapshot: %w", err)
	}

	path := s.snapshotPath(rt.Config.ID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create roundtables dir: %w", err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write snapshot: %w", err)
	}

	return nil
}

// Load reconstructs a Roundtable from its persisted snapshot.
func (s *Store) Load(ctx context.Context, id string) (*Roundtable, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	path := s.snapshotPath(id)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read snapshot: %w", err)
	}

	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("unmarshal snapshot: %w", err)
	}

	if snap.Usage.ByRole == nil {
		snap.Usage.ByRole = make(map[string]RoleUsage)
	}

	rt := &Roundtable{
		Config:       snap.Config,
		Phase:        snap.Phase,
		Transcript:   snap.Transcript,
		CurrentSeq:   snap.CurrentSeq,
		CurrentTurn:  snap.CurrentTurn,
		Usage:        snap.Usage,
		Consensus:    snap.Consensus,
		MaxTurnsHit:  snap.MaxTurnsHit,
		LoopDetected: snap.LoopDetected,
	}

	return rt, nil
}

// ListSessions returns the IDs of all saved roundtable sessions.
func (s *Store) ListSessions(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	dir := s.roundtablesDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list roundtables dir: %w", err)
	}

	var ids []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(e.Name(), ".json"))
	}
	return ids, nil
}

// Delete removes a persisted roundtable snapshot.
func (s *Store) Delete(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Remove(s.snapshotPath(id))
}
