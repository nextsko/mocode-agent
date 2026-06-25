package gitopscommon

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// FileStatus represents the git status of a single file.
type FileStatus int

const (
	StatusModified FileStatus = iota
	StatusAdded
	StatusDeleted
	StatusRenamed
	StatusCopied
	StatusUntracked
)

func (s FileStatus) String() string {
	switch s {
	case StatusModified:
		return "modified"
	case StatusAdded:
		return "added"
	case StatusDeleted:
		return "deleted"
	case StatusRenamed:
		return "renamed"
	case StatusCopied:
		return "copied"
	case StatusUntracked:
		return "untracked"
	default:
		return "unknown"
	}
}

// FileChange represents a single changed file in the working tree.
type FileChange struct {
	Path       string     `json:"path"`
	Status     FileStatus `json:"status"`
	Insertions int        `json:"insertions"`
	Deletions  int        `json:"deletions"`
}

// ScanResult holds the complete working tree scan output.
type ScanResult struct {
	Branch    string       `json:"branch"`
	Ahead     int          `json:"ahead"`
	Files     []FileChange `json:"files"`
	TotalAdd  int          `json:"total_add"`
	TotalDel  int          `json:"total_del"`
	StagedLen int          `json:"staged_count"`
}

// ScanWorkingTree runs git commands to produce a full working tree snapshot.
func ScanWorkingTree(dir string) (*ScanResult, error) {
	branch, ahead, err := scanBranch(dir)
	if err != nil {
		return nil, fmt.Errorf("scan branch: %w", err)
	}

	changes, err := scanChanges(dir)
	if err != nil {
		return nil, fmt.Errorf("scan changes: %w", err)
	}

	staged, err := scanStagedCount(dir)
	if err != nil {
		return nil, fmt.Errorf("scan staged: %w", err)
	}

	result := &ScanResult{
		Branch:    branch,
		Ahead:     ahead,
		Files:     changes,
		StagedLen: staged,
	}
	for _, f := range changes {
		result.TotalAdd += f.Insertions
		result.TotalDel += f.Deletions
	}
	return result, nil
}

// scanBranch returns the current branch name and how many commits it is ahead
// of the upstream tracking branch.
func scanBranch(dir string) (string, int, error) {
	out, err := GitRun(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", 0, err
	}
	branch := strings.TrimSpace(string(out))

	ahead := 0
	upstream, err := GitRun(dir, "rev-parse", "--abbrev-ref", "@{upstream}")
	if err == nil {
		revList, err := GitRun(dir, "rev-list", "--count",
			strings.TrimSpace(string(upstream))+"..HEAD")
		if err == nil {
			ahead, _ = strconv.Atoi(strings.TrimSpace(string(revList)))
		}
	}
	return branch, ahead, nil
}

// scanChanges reads git status --porcelain=v1 and git diff --numstat to
// produce the full list of changed files with insertion/deletion counts.
func scanChanges(dir string) ([]FileChange, error) {
	// Step 1: git status --porcelain=v1 to collect file paths and status codes
	statusOut, err := GitRun(dir, "status", "--porcelain=v1")
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(statusOut))) == 0 {
		return nil, nil
	}

	// Parse status lines: XY <path> (or XY <orig> -> <new> for renames)
	statusMap := make(map[string]FileStatus)
	var paths []string
	for _, line := range strings.Split(string(statusOut), "\n") {
		if len(line) < 4 {
			continue
		}
		code := strings.TrimSpace(line[:2])
		path := strings.TrimSpace(line[3:])

		// Handle renames: "old -> new"
		if strings.Contains(path, " -> ") {
			parts := strings.SplitN(path, " -> ", 2)
			path = parts[1]
		}

		var st FileStatus
		switch {
		case code == "??":
			st = StatusUntracked
		case code == "A":
			st = StatusAdded
		case code == "D":
			st = StatusDeleted
		case strings.HasPrefix(code, "R"):
			st = StatusRenamed
		case strings.HasPrefix(code, "C"):
			st = StatusCopied
		default:
			st = StatusModified
		}

		statusMap[path] = st
		paths = append(paths, path)
	}

	// Step 2: git diff --numstat for insertion/deletion counts
	diffOut, _ := GitRun(dir, "diff", "--numstat")
	numstatMap := make(map[string][2]int)
	for _, line := range strings.Split(string(diffOut), "\n") {
		parts := strings.Fields(line)
		if len(parts) < 3 {
			continue
		}
		add, _ := strconv.Atoi(parts[0])
		del, _ := strconv.Atoi(parts[1])
		numstatMap[parts[2]] = [2]int{add, del}
	}

	// Step 3: Merge status + numstat
	var changes []FileChange
	seen := make(map[string]bool)
	for _, p := range paths {
		if seen[p] {
			continue
		}
		seen[p] = true
		fc := FileChange{
			Path:   p,
			Status: statusMap[p],
		}
		if nums, ok := numstatMap[p]; ok {
			fc.Insertions = nums[0]
			fc.Deletions = nums[1]
		}
		changes = append(changes, fc)
	}

	return changes, nil
}

// scanStagedCount returns the number of files in the staging area.
func scanStagedCount(dir string) (int, error) {
	out, err := GitRun(dir, "diff", "--cached", "--name-only")
	if err != nil {
		return 0, err
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return 0, nil
	}
	return len(strings.Split(trimmed, "\n")), nil
}

// DiffStat returns the raw numstat for a single file against HEAD.
func DiffStat(dir, path string) (additions, deletions int) {
	out, err := GitRun(dir, "diff", "--numstat", "HEAD", "--", path)
	if err != nil {
		return 0, 0
	}
	parts := strings.Fields(strings.TrimSpace(string(out)))
	if len(parts) >= 2 {
		additions, _ = strconv.Atoi(parts[0])
		deletions, _ = strconv.Atoi(parts[1])
	}
	return
}

// GitRun executes a git sub-command in dir and returns combined stdout.
func GitRun(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("git %s: %w (%s)",
			strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}
