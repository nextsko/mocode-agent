package skills

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Provenance metadata keys recorded in the frontmatter of an agent-authored
// skill. They are plain `metadata` entries, so the SKILL.md stays valid under
// the Agent Skills open standard and older readers keep working.
//
// Provenance is what lets future curation (archival, merge, rollback) treat
// machine-written content conservatively: a skill whose origin is OriginLearn
// may be rewritten or reorganised automatically, while a hand-written one may
// not. See docs/design/09-hermes-and-claude-code-evolution.md.
const (
	// MetaOrigin records who authored the skill (see OriginLearn).
	MetaOrigin = "origin"
	// MetaRevision is a monotonically increasing counter, bumped on every rewrite.
	MetaRevision = "revision"
	// MetaCreatedAt is the RFC3339 UTC creation timestamp.
	MetaCreatedAt = "created_at"
	// MetaUpdatedAt is the RFC3339 UTC timestamp of the last rewrite.
	MetaUpdatedAt = "updated_at"
	// MetaReason records why the skill was created or last revised.
	MetaReason = "reason"

	// OriginLearn marks a skill authored by the learn tool.
	OriginLearn = "learn"
)

// BackupDirName is the hidden directory, relative to a skills root, that holds
// pre-overwrite copies. Backups are deliberately stored one level below the
// root and never named [SkillFileName], so [Discover] can not pick them up as
// live skills.
const BackupDirName = ".backups"

// WriteResult describes the outcome of a successful [WriteFile].
type WriteResult struct {
	// Path is the absolute path of the written SKILL.md.
	Path string
	// Dir is the skill's directory (the parent of Path).
	Dir string
	// Backup is the path of the copy taken before an overwrite, or "" when
	// the target did not previously exist.
	Backup string
	// Overwrote reports whether a pre-existing SKILL.md was replaced.
	Overwrote bool
}

// frontmatterDoc mirrors [Skill]'s serialisable fields in a stable key order.
// It exists so Render emits a deterministic frontmatter block rather than
// whatever order the map-of-metadata happens to produce.
type frontmatterDoc struct {
	Name          string            `yaml:"name"`
	Description   string            `yaml:"description"`
	License       string            `yaml:"license,omitempty"`
	Compatibility string            `yaml:"compatibility,omitempty"`
	Metadata      map[string]string `yaml:"metadata,omitempty"`
}

// Render serialises a skill back to SKILL.md bytes: a YAML frontmatter block
// delimited by `---` fences, a blank line, then the markdown body.
//
// Values are emitted through the YAML marshaller, so descriptions containing
// `:` or newlines are quoted automatically and can always be read back by
// [ParseContent].
func Render(s *Skill) ([]byte, error) {
	if s == nil {
		return nil, errors.New("skill is nil")
	}

	var meta map[string]string
	if len(s.Metadata) > 0 {
		meta = s.Metadata
	}

	fm, err := yaml.Marshal(frontmatterDoc{
		Name:          s.Name,
		Description:   s.Description,
		License:       s.License,
		Compatibility: s.Compatibility,
		Metadata:      meta,
	})
	if err != nil {
		return nil, fmt.Errorf("encoding frontmatter: %w", err)
	}

	var sb strings.Builder
	sb.WriteString("---\n")
	sb.Write(fm)
	sb.WriteString("---\n")

	if body := strings.TrimSpace(s.Instructions); body != "" {
		sb.WriteString("\n")
		sb.WriteString(body)
		sb.WriteString("\n")
	}

	return []byte(sb.String()), nil
}

// WriteFile validates s and writes it to <root>/<name>/SKILL.md.
//
// The write is atomic — content goes to a temp file in the destination
// directory and is then renamed over the target — so a crash or a failed
// validation can never leave a truncated skill on disk. When the target
// already exists it is first copied to <root>/.backups/<name>/<timestamp>.md,
// which makes every overwrite recoverable.
//
// The returned [WriteResult.Backup] is empty when nothing was replaced.
func WriteFile(root string, s *Skill, now time.Time) (WriteResult, error) {
	if s == nil {
		return WriteResult{}, errors.New("skill is nil")
	}
	if strings.TrimSpace(root) == "" {
		return WriteResult{}, errors.New("skills root is empty")
	}
	return writeSkillDir(filepath.Join(filepath.Clean(root), s.Name), s, now)
}

// WriteFileTo writes s into dir, the skill's own directory. Use it to rewrite a
// skill in place — for example a skill that was discovered in a directory other
// than the caller's default root — instead of creating a shadowing copy.
//
// dir's base name must equal s.Name; [Validate] enforces that, so a skill can
// never be written to a directory that would make it undiscoverable.
func WriteFileTo(dir string, s *Skill, now time.Time) (WriteResult, error) {
	if s == nil {
		return WriteResult{}, errors.New("skill is nil")
	}
	if strings.TrimSpace(dir) == "" {
		return WriteResult{}, errors.New("skill directory is empty")
	}
	return writeSkillDir(filepath.Clean(dir), s, now)
}

// writeSkillDir implements the shared create-or-replace flow. dir is the
// skill's own directory; backups are kept beside it in <parent>/.backups/<name>.
func writeSkillDir(dir string, s *Skill, now time.Time) (WriteResult, error) {
	if err := s.Validate(); err != nil {
		return WriteResult{}, fmt.Errorf("skill is invalid: %w", err)
	}

	// Re-validate against the real destination so the "name must match its
	// directory" rule is checked against where the file will actually land.
	probe := *s
	probe.Path = dir
	if err := probe.Validate(); err != nil {
		return WriteResult{}, fmt.Errorf("skill is invalid at %s: %w", dir, err)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return WriteResult{}, fmt.Errorf("creating skill directory %s: %w", dir, err)
	}

	target := filepath.Join(dir, SkillFileName)
	res := WriteResult{Path: target, Dir: dir}

	switch _, err := os.Stat(target); {
	case err == nil:
		backup, berr := backupExisting(filepath.Dir(dir), s.Name, target, now)
		if berr != nil {
			return WriteResult{}, berr
		}
		res.Backup = backup
		res.Overwrote = true
	case !os.IsNotExist(err):
		return WriteResult{}, fmt.Errorf("stat %s: %w", target, err)
	}

	data, err := Render(s)
	if err != nil {
		return WriteResult{}, err
	}

	// Round-trip guard: never persist bytes we cannot read back.
	if _, err := ParseContent(data); err != nil {
		return WriteResult{}, fmt.Errorf("rendered skill failed to parse: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".skill-*.tmp")
	if err != nil {
		return WriteResult{}, fmt.Errorf("creating temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	// No-op once the rename below has moved the file.
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return WriteResult{}, fmt.Errorf("writing %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return WriteResult{}, fmt.Errorf("closing %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, target); err != nil {
		return WriteResult{}, fmt.Errorf("replacing %s: %w", target, err)
	}

	return res, nil
}

// backupExisting copies the current SKILL.md into the root's backup area and
// returns the path of the copy. Backups are written as `<timestamp>.md` rather
// than SKILL.md so [DiscoverWithStates] ignores them.
func backupExisting(root, name, target string, now time.Time) (string, error) {
	data, err := os.ReadFile(target)
	if err != nil {
		return "", fmt.Errorf("reading existing skill %s: %w", target, err)
	}

	dir := filepath.Join(root, BackupDirName, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating backup directory %s: %w", dir, err)
	}

	stamp := now.UTC().Format("20060102T150405Z")
	dst := filepath.Join(dir, stamp+".md")
	// Two rewrites inside the same second must not clobber each other.
	for i := 1; ; i++ {
		if _, err := os.Stat(dst); os.IsNotExist(err) {
			break
		}
		dst = filepath.Join(dir, stamp+"-"+strconv.Itoa(i)+".md")
	}

	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return "", fmt.Errorf("writing backup %s: %w", dst, err)
	}
	return dst, nil
}
