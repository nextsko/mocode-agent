package flywheel

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type skillIndex struct {
	Skills []Skill `json:"skills"`
}

func skillsDir(workingDir string) string { return filepath.Join(Dir(workingDir), "skills") }

// LoadSkills reads the skill registry.
func LoadSkills(workingDir string) ([]Skill, error) {
	raw, err := os.ReadFile(filepath.Join(skillsDir(workingDir), "index.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read skill index: %w", err)
	}
	var idx skillIndex
	if err := json.Unmarshal(raw, &idx); err != nil {
		return nil, fmt.Errorf("parse skill index: %w", err)
	}
	return idx.Skills, nil
}

func saveSkills(skills []Skill, workingDir string) error {
	dir := skillsDir(workingDir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create skills dir: %w", err)
	}
	raw, err := json.MarshalIndent(skillIndex{Skills: skills}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal skills: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, "index.json"), raw, 0o600)
}

// UpsertSkill applies the flywheel promotion rules: create when nothing
// similar exists, upgrade when the path gets shorter, update when only new
// lessons/steps are merged, and skip when the draft adds nothing.
func UpsertSkill(draft Draft, workingDir string) (Decision, error) {
	skills, err := LoadSkills(workingDir)
	if err != nil {
		return Decision{}, err
	}
	now := time.Now().UTC()
	idx := matchSkill(skills, draft)
	if idx < 0 {
		s := Skill{
			Name: draft.Name, Type: draft.Type, Description: draft.Description,
			Triggers: draft.Triggers, Instructions: draft.Instructions, Path: draft.Path,
			Version: 1, UpdatedAt: now,
			History: []SkillVersion{{Version: 1, At: now, Reason: "created from task", PathSteps: len(draft.Path)}},
		}
		skills = append(skills, s)
		if err := saveSkills(skills, workingDir); err != nil {
			return Decision{}, err
		}
		if err := writeSkillFile(s, workingDir); err != nil {
			return Decision{}, err
		}
		return Decision{Kind: DecisionCreated, Skill: &skills[len(skills)-1], Reason: "no similar skill existed", Version: 1}, nil
	}

	cur := &skills[idx]
	improved := len(draft.Path) > 0 && (len(cur.Path) == 0 || len(draft.Path) < len(cur.Path))
	newInfo := hasNewInfo(*cur, draft)
	switch {
	case !improved && !newInfo:
		return Decision{Kind: DecisionSkipped, Skill: cur, Reason: "draft adds no shorter path and no new steps/lessons", Version: cur.Version}, nil
	case improved:
		cur.Path = draft.Path
		cur.Instructions = draft.Instructions
		cur.Triggers = mergeStrings(cur.Triggers, draft.Triggers)
		cur.Version++
		cur.UpdatedAt = now
		cur.History = append(cur.History, SkillVersion{Version: cur.Version, At: now, Reason: fmt.Sprintf("path shortened to %d steps", len(draft.Path)), PathSteps: len(draft.Path)})
		if err := persistSkill(*cur, workingDir); err != nil {
			return Decision{}, err
		}
		return Decision{Kind: DecisionUpgraded, Skill: cur, Reason: fmt.Sprintf("path %d → %d steps", len(cur.Path), len(draft.Path)), Version: cur.Version}, nil
	default:
		cur.Instructions = draft.Instructions
		cur.Triggers = mergeStrings(cur.Triggers, draft.Triggers)
		cur.Version++
		cur.UpdatedAt = now
		cur.History = append(cur.History, SkillVersion{Version: cur.Version, At: now, Reason: "merged new lessons/steps", PathSteps: len(cur.Path)})
		if err := persistSkill(*cur, workingDir); err != nil {
			return Decision{}, err
		}
		return Decision{Kind: DecisionUpdated, Skill: cur, Reason: "merged new lessons/steps", Version: cur.Version}, nil
	}
}

// RecordUse updates reuse statistics; the model calls it when it applies a
// skill so the flywheel can tell which skills actually earn their keep.
func RecordUse(name string, success bool, workingDir string) (Skill, bool, error) {
	skills, err := LoadSkills(workingDir)
	if err != nil {
		return Skill{}, false, err
	}
	for i := range skills {
		if !strings.EqualFold(skills[i].Name, name) {
			continue
		}
		skills[i].Uses++
		if success {
			skills[i].Successes++
		} else {
			skills[i].Failures++
		}
		skills[i].UpdatedAt = time.Now().UTC()
		if err := persistSkill(skills[i], workingDir); err != nil {
			return Skill{}, false, err
		}
		return skills[i], true, nil
	}
	return Skill{}, false, nil
}

func persistSkill(s Skill, workingDir string) error {
	skills, err := LoadSkills(workingDir)
	if err != nil {
		return err
	}
	found := false
	for i := range skills {
		if skills[i].Name == s.Name {
			skills[i] = s
			found = true
		}
	}
	if !found {
		skills = append(skills, s)
	}
	if err := saveSkills(skills, workingDir); err != nil {
		return err
	}
	return writeSkillFile(s, workingDir)
}

func writeSkillFile(s Skill, workingDir string) error {
	dir := filepath.Join(skillsDir(workingDir), s.Name)
	if err := os.MkdirAll(filepath.Join(dir, "versions"), 0o750); err != nil {
		return fmt.Errorf("create skill dir: %w", err)
	}
	doc := fmt.Sprintf("---\nname: %s\ndescription: %s\nversion: %d\n---\n\n# %s\n\n%s\n", s.Name, s.Description, s.Version, s.Name, s.Instructions)
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(doc), 0o600); err != nil {
		return fmt.Errorf("write skill: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, "versions", fmt.Sprintf("v%d.md", s.Version)), []byte(doc), 0o600)
}

func matchSkill(skills []Skill, draft Draft) int {
	best, bestScore := -1, 0.0
	for i, s := range skills {
		score := 0.0
		if strings.EqualFold(s.Type, draft.Type) {
			score += 0.4
		}
		if strings.EqualFold(s.Name, draft.Name) {
			score += 0.4
		} else {
			score += 0.2 * jaccard(triggersOfDraft(s.Triggers), triggersOfDraft(draft.Triggers))
		}
		if score > bestScore {
			bestScore, best = score, i
		}
	}
	if bestScore >= 0.6 {
		return best
	}
	return -1
}

func triggersOfDraft(in []string) map[string]bool {
	out := map[string]bool{}
	for _, t := range in {
		if t = strings.ToLower(strings.TrimSpace(t)); t != "" {
			out[t] = true
		}
	}
	return out
}

func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for k := range a {
		if b[k] {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func hasNewInfo(cur Skill, draft Draft) bool {
	if len(draft.Lessons) > len(cur.History) {
		return true
	}
	known := map[string]bool{}
	for _, s := range cur.Path {
		known[s.Action+"|"+s.Target+"|"+s.Command] = true
	}
	for _, s := range draft.Path {
		if !known[s.Action+"|"+s.Target+"|"+s.Command] {
			return true
		}
	}
	return false
}

func mergeStrings(a, b []string) []string {
	set := map[string]bool{}
	var out []string
	for _, v := range append(append([]string{}, a...), b...) {
		if v == "" || set[v] {
			continue
		}
		set[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
