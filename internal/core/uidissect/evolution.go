package uidissect

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// EvolutionEntry records one regression run: did the change improve the score?
type EvolutionEntry struct {
	At         time.Time `json:"at"`
	BaseURL    string    `json:"base_url"`
	CurrentURL string    `json:"current_url"`
	BaseScore  int       `json:"base_score"`
	CurScore   int       `json:"cur_score"`
	Delta      int       `json:"delta"`
	Changes    int       `json:"changes"`
}

// EvolutionStats summarises the improvement history.
type EvolutionStats struct {
	Runs      int     `json:"runs"`
	Improved  int     `json:"improved"`
	Degraded  int     `json:"degraded"`
	Unchanged int     `json:"unchanged"`
	AvgDelta  float64 `json:"avg_delta"`
}

// AppendEvolution appends one entry to .mocode/ui/evolution.jsonl.
func AppendEvolution(e EvolutionEntry, workingDir string) error {
	dir := Dir(workingDir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create ui dir: %w", err)
	}
	raw, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshal evolution: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(dir, "evolution.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open evolution log: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(raw, '\n')); err != nil {
		return fmt.Errorf("append evolution: %w", err)
	}
	return nil
}

// LoadEvolution reads the full improvement history.
func LoadEvolution(workingDir string) ([]EvolutionEntry, error) {
	raw, err := os.ReadFile(filepath.Join(Dir(workingDir), "evolution.jsonl"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read evolution: %w", err)
	}
	var out []EvolutionEntry
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e EvolutionEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

// SummarizeEvolution computes improvement statistics.
func SummarizeEvolution(entries []EvolutionEntry) EvolutionStats {
	s := EvolutionStats{Runs: len(entries)}
	total := 0
	for _, e := range entries {
		total += e.Delta
		switch {
		case e.Delta > 0:
			s.Improved++
		case e.Delta < 0:
			s.Degraded++
		default:
			s.Unchanged++
		}
	}
	if len(entries) > 0 {
		s.AvgDelta = round1(float64(total) / float64(len(entries)))
	}
	return s
}

// RenderEvolution renders the history as a table.
func RenderEvolution(entries []EvolutionEntry) string {
	if len(entries) == 0 {
		return "_No regression runs recorded yet._\n"
	}
	var b strings.Builder
	b.WriteString("| At | Base | Current | Score | Δ | Changes |\n|---|---|---|---|---|---|\n")
	start := 0
	if len(entries) > 30 {
		start = len(entries) - 30
	}
	for _, e := range entries[start:] {
		sign := ""
		if e.Delta > 0 {
			sign = "+"
		}
		fmt.Fprintf(&b, "| %s | %d | %d | %s%d | %s%d | %d |\n",
			e.At.Format("01-02 15:04"), e.BaseScore, e.CurScore,
			"", e.CurScore, sign, e.Delta, e.Changes)
	}
	return b.String()
}
