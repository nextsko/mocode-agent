package gitopscommon

import (
	"path/filepath"
	"strings"
)

// CommitType is the conventional-commit type.
type CommitType string

const (
	TypeFeat     CommitType = "feat"
	TypeFix      CommitType = "fix"
	TypeRefactor CommitType = "refactor"
	TypeTest     CommitType = "test"
	TypeDocs     CommitType = "docs"
	TypeChore    CommitType = "chore"
	TypeConfig   CommitType = "config"
	TypeBuild    CommitType = "build"
	TypeStyle    CommitType = "style"
	TypePerf     CommitType = "perf"
	TypeCI       CommitType = "ci"
	TypeUI       CommitType = "ui"
)

// emojiMap maps CommitType to its emoji prefix.
var emojiMap = map[CommitType]string{
	TypeFeat:     "✨",
	TypeFix:      "🐛",
	TypeRefactor: "♻️",
	TypeTest:     "✅",
	TypeDocs:     "📝",
	TypeChore:    "🔧",
	TypeConfig:   "⚙️",
	TypeBuild:    "📦",
	TypeStyle:    "💄",
	TypePerf:     "⚡",
	TypeCI:       "🔰",
	TypeUI:       "🎨",
}

// priorityMap defines the commit ordering: lower = earlier.
var priorityMap = map[CommitType]int{
	TypeDocs:     0,
	TypeFix:      1,
	TypeFeat:     2,
	TypeUI:       2,
	TypeRefactor: 3,
	TypeTest:     4,
	TypeConfig:   5,
	TypeChore:    5,
	TypeBuild:    6,
	TypeStyle:    7,
	TypePerf:     7,
	TypeCI:       7,
}

// FileClass holds the inferred type and scope for a single file.
type FileClass struct {
	Path  string     `json:"path"`
	Type  CommitType `json:"type"`
	Scope string     `json:"scope"`
}

// Classify infers type and scope for a file based on its path and status.
func Classify(fc FileChange) FileClass {
	c := FileClass{Path: fc.Path}

	// ── Scope inference from path ──
	c.Scope = inferScope(fc.Path)

	// ── Type inference ──
	ext := strings.ToLower(filepath.Ext(fc.Path))
	base := strings.ToLower(filepath.Base(fc.Path))
	dir := strings.ToLower(filepath.Dir(fc.Path))

	switch {
	// Test files (highest priority)
	case isTestFile(fc.Path):
		c.Type = TypeTest

	// Pure docs — even new .md files are docs, not feat
	case isDoc(fc.Path):
		c.Type = TypeDocs

	// CI/CD
	case isCI(fc.Path):
		c.Type = TypeCI

	// Build / dependency files
	case isBuild(fc.Path, ext, base):
		c.Type = TypeChore
		c.Scope = "deps"

	// Config files
	case isConfig(fc.Path, ext, base, dir):
		c.Type = TypeConfig

	// Untracked → feat (new file, after doc/build checks)
	case fc.Status == StatusUntracked:
		c.Type = TypeFeat

	// Deleted-only → refactor (dead code removal)
	case fc.Status == StatusDeleted:
		c.Type = TypeRefactor

	// Go source → type depends on scope
	case ext == ".go":
		c.Type = inferGoType(c.Scope, fc)

	// Default
	default:
		c.Type = TypeChore
	}

	// Upgrade scope-level patterns
	if c.Scope == "config" && c.Type == TypeChore {
		c.Type = TypeConfig
	}
	// Admin UI changes are features by default
	if c.Scope == "admin" && c.Type == TypeChore {
		c.Type = TypeFeat
	}

	return c
}

// inferScope determines the conventional-commit scope from a file path.
func inferScope(path string) string {
	p := filepath.ToSlash(path)

	switch {
	case strings.HasPrefix(p, "internal/config/"):
		return "config"
	case strings.HasPrefix(p, "internal/admin/"):
		return "admin"
	case strings.HasPrefix(p, "internal/agent/"):
		if strings.Contains(p, "/tools/") {
			return "tools"
		}
		return "agent"
	case strings.HasPrefix(p, "internal/ui/"):
		return "ui"
	case strings.HasPrefix(p, "internal/session/"):
		return "session"
	case strings.HasPrefix(p, "internal/lsp/"):
		return "lsp"
	case strings.HasPrefix(p, "internal/hooks/"):
		return "hooks"
	case strings.HasPrefix(p, "internal/skills/"):
		return "skills"
	case strings.HasPrefix(p, "internal/wechat/"):
		return "wechat"
	case strings.HasPrefix(p, "internal/store/"):
		return "store"
	case strings.HasPrefix(p, ".github/"):
		return "ci"
	case strings.HasPrefix(p, "internal/"):
		// Generic internal scope
		parts := strings.SplitN(strings.TrimPrefix(p, "internal/"), "/", 2)
		if len(parts) > 0 && parts[0] != "" {
			return parts[0]
		}
	}
	return ""
}

// ── Type inference helpers ─────────────────────────────────────────────────────

func inferGoType(scope string, fc FileChange) CommitType {
	if fc.Status == StatusDeleted {
		return TypeRefactor
	}
	switch scope {
	case "config":
		return TypeConfig
	case "ui":
		return TypeUI
	default:
		// If new file → feat
		if fc.Status == StatusAdded || fc.Status == StatusUntracked {
			return TypeFeat
		}
		return TypeRefactor
	}
}

func isTestFile(path string) bool {
	lower := strings.ToLower(filepath.Base(path))
	return strings.HasSuffix(lower, "_test.go") ||
		strings.HasSuffix(lower, "_test.py") ||
		strings.HasSuffix(lower, ".test.ts") ||
		strings.HasSuffix(lower, ".test.tsx") ||
		strings.HasSuffix(lower, ".test.js") ||
		strings.HasSuffix(lower, ".test.jsx") ||
		strings.HasSuffix(lower, "_test.rs") ||
		strings.Contains(lower, "testdata") ||
		strings.Contains(filepath.ToSlash(path), "/test/")
}

func isDoc(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	base := strings.ToLower(filepath.Base(path))
	p := filepath.ToSlash(path)
	return ext == ".md" || ext == ".txt" || ext == ".rst" || ext == ".tpl" ||
		base == "license" || base == "changelog" ||
		strings.HasPrefix(p, "docs/")
}

func isCI(path string) bool {
	p := filepath.ToSlash(path)
	return strings.HasPrefix(p, ".github/") ||
		strings.HasPrefix(p, ".gitlab-ci") ||
		strings.HasPrefix(p, ".circleci/") ||
		strings.HasPrefix(p, ".travis") ||
		filepath.Base(path) == "Dockerfile" ||
		filepath.Base(path) == "docker-compose.yml"
}

func isBuild(path, ext, base string) bool {
	return base == "go.mod" || base == "go.sum" ||
		base == "package.json" || base == "package-lock.json" ||
		base == "yarn.lock" || base == "pnpm-lock.yaml" ||
		base == "Makefile" || base == "Taskfile.yml" ||
		base == "Cargo.toml" || base == "Cargo.lock" ||
		ext == ".goreleaser.yml" || ext == ".goreleaser.yaml"
}

func isConfig(path, ext, base, dir string) bool {
	return ext == ".toml" || ext == ".yaml" || ext == ".yml" ||
		ext == ".json" && !strings.Contains(dir, "node_modules") ||
		base == ".env" || strings.HasPrefix(base, ".env.") ||
		base == ".gitignore" || base == ".editorconfig" ||
		strings.HasSuffix(base, "-schema.json")
}
