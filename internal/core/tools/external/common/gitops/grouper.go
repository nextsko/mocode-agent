package gitopscommon

import (
	"fmt"
	"sort"
	"strings"
)

const maxFilesPerGroup = 8

// GroupKey uniquely identifies a commit group by type + scope.
type GroupKey struct {
	Type  CommitType
	Scope string
}

// CommitGroup is a proposed commit containing related files.
type CommitGroup struct {
	Index     int      `json:"index"`
	Type      string   `json:"type"`
	Scope     string   `json:"scope"`
	Emoji     string   `json:"emoji"`
	Files     []string `json:"files"`
	Additions int      `json:"additions"`
	Deletions int      `json:"deletions"`
	Subject   string   `json:"subject"`
	Body      string   `json:"body"`
	Priority  int      `json:"priority"`
}

// CommitPlan is the top-level result returned by plan_commits.
type CommitPlan struct {
	Branch      string        `json:"branch"`
	Ahead       int           `json:"ahead"`
	TotalFiles  int           `json:"total_files"`
	TotalAdd    int           `json:"total_add"`
	TotalDel    int           `json:"total_del"`
	Groups      []CommitGroup `json:"groups"`
	Summary     string        `json:"summary"`
	CommitOrder []int         `json:"commit_order"`
}

// GroupFiles classifies and groups changed files into CommitGroups.
// Each group shares the same type + scope. Large groups are split by
// sub-directory when they exceed maxFilesPerGroup.
func GroupFiles(result *ScanResult) *CommitPlan {
	plan := &CommitPlan{
		Branch:     result.Branch,
		Ahead:      result.Ahead,
		TotalFiles: len(result.Files),
		TotalAdd:   result.TotalAdd,
		TotalDel:   result.TotalDel,
	}

	// Step 1: Classify every file
	var classes []FileClass
	classChanges := make(map[FileClass][]FileChange)
	for _, fc := range result.Files {
		cl := Classify(fc)
		classes = append(classes, cl)
		classChanges[cl] = append(classChanges[cl], fc)
	}

	// Step 2: Group by type+scope
	groups := make(map[GroupKey]*CommitGroup)
	var keyOrder []GroupKey

	for _, cl := range classes {
		key := GroupKey{Type: cl.Type, Scope: cl.Scope}
		if _, exists := groups[key]; !exists {
			groups[key] = &CommitGroup{
				Type:  string(cl.Type),
				Scope: cl.Scope,
				Emoji: emojiMap[cl.Type],
			}
			keyOrder = append(keyOrder, key)
		}
		g := groups[key]
		fc := findChange(result.Files, cl.Path)
		g.Files = append(g.Files, cl.Path)
		g.Additions += fc.Insertions
		g.Deletions += fc.Deletions
	}

	// Step 3: Flatten groups into slice, splitting large ones
	var out []CommitGroup
	idx := 0
	for _, key := range keyOrder {
		g := groups[key]
		split := splitLargeGroup(g)
		for _, sg := range split {
			sg.Index = idx
			sg.Priority = priorityMap[CommitType(sg.Type)]
			out = append(out, *sg)
			idx++
		}
	}

	// Step 4: Sort by priority
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority < out[j].Priority
		}
		return out[i].Scope < out[j].Scope
	})

	// Re-index after sort
	for i := range out {
		out[i].Index = i
	}

	plan.Groups = out

	// Build commit order
	order := make([]int, len(out))
	for i := range out {
		order[i] = i
	}
	plan.CommitOrder = order

	// Build summary
	plan.Summary = buildSummary(plan)

	return plan
}

// splitLargeGroup breaks a group into smaller pieces if it has too many files.
// It groups by sub-directory prefix.
func splitLargeGroup(g *CommitGroup) []*CommitGroup {
	if len(g.Files) <= maxFilesPerGroup {
		return []*CommitGroup{g}
	}

	// Split by first two directory levels
	buckets := make(map[string][]string)
	for _, f := range g.Files {
		parts := strings.SplitN(f, "/", 3)
		bucket := ""
		if len(parts) >= 2 {
			bucket = parts[0] + "/" + parts[1]
		} else {
			bucket = parts[0]
		}
		buckets[bucket] = append(buckets[bucket], f)
	}

	// If only one bucket, split by half
	if len(buckets) <= 1 {
		half := len(g.Files) / 2
		g1 := *g
		g1.Files = g.Files[:half]
		g2 := *g
		g2.Files = g.Files[half:]
		return []*CommitGroup{&g1, &g2}
	}

	var result []*CommitGroup
	for _, files := range buckets {
		sg := *g
		sg.Files = files
		result = append(result, &sg)
	}

	// Sort by bucket name for deterministic output
	sort.Slice(result, func(i, j int) bool {
		return result[i].Files[0] < result[j].Files[0]
	})

	return result
}

// findChange locates the FileChange for a given path.
func findChange(changes []FileChange, path string) FileChange {
	for _, fc := range changes {
		if fc.Path == path {
			return fc
		}
	}
	return FileChange{Path: path}
}

// buildSummary generates a one-line human-readable summary.
func buildSummary(p *CommitPlan) string {
	return fmt.Sprintf("%s: %d files (+%d -%d) across %d commit groups",
		p.Branch, p.TotalFiles, p.TotalAdd, p.TotalDel, len(p.Groups))
}

// FormatPlan renders the plan as a human-readable tree for the agent.
func FormatPlan(p *CommitPlan) string {
	var b strings.Builder

	b.WriteString("📦 Commit Plan\n")
	b.WriteString(fmt.Sprintf("├── Branch: %s", p.Branch))
	if p.Ahead > 0 {
		b.WriteString(fmt.Sprintf(" (ahead %d)", p.Ahead))
	}
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("├── Files: %d (+%d -%d)\n", p.TotalFiles, p.TotalAdd, p.TotalDel))
	b.WriteString(fmt.Sprintf("└── Groups: %d\n\n", len(p.Groups)))

	for i, g := range p.Groups {
		connector := "├──"
		if i == len(p.Groups)-1 {
			connector = "└──"
		}
		scope := g.Scope
		if scope == "" {
			scope = "general"
		}
		b.WriteString(fmt.Sprintf("%s %s %s(%s): [%d files] +%d -%d\n",
			connector, g.Emoji, g.Type, scope,
			len(g.Files), g.Additions, g.Deletions))
		for j, f := range g.Files {
			fileConnector := "│   ├──"
			if j == len(g.Files)-1 {
				fileConnector = "│   └──"
			}
			b.WriteString(fmt.Sprintf("    %s %s\n", fileConnector, f))
		}
		b.WriteString("\n")
	}

	b.WriteString("Suggested commit order: docs → fix → feat → refactor → test → config/chore → build\n")
	b.WriteString("\nFill in `subject` and `body` for each group, then call git_execute_commits.\n")

	return b.String()
}
