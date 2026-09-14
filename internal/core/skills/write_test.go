package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fixedNow() time.Time {
	return time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
}

func sampleSkill() *Skill {
	return &Skill{
		Name:         "deploy-runbook",
		Description:  "Use when deploying mocode to a new host",
		Instructions: "# Deploy\n\n1. Run `task dev:ship`.\n",
		Metadata: map[string]string{
			MetaOrigin:    OriginLearn,
			MetaRevision:  "1",
			MetaCreatedAt: fixedNow().Format(time.RFC3339),
			MetaUpdatedAt: fixedNow().Format(time.RFC3339),
		},
	}
}

// TestRender_RoundTrips is the load-bearing invariant of the writer: whatever
// we persist must parse back into an equivalent skill. If this breaks, learned
// skills silently stop being discovered.
func TestRender_RoundTrips(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		skill *Skill
	}{
		{"plain", sampleSkill()},
		{
			// Descriptions are free text from a model, so colons, quotes and
			// newlines must survive YAML encoding.
			name: "tricky description",
			skill: &Skill{
				Name:         "tricky",
				Description:  `Use when the config has "key: value" pairs, retries: enabled, and a #comment`,
				Instructions: "Body with `---` inside:\n\n---\n\nnot frontmatter\n",
			},
		},
		{"colon only", &Skill{Name: "a-colon", Description: "Use when x: y", Instructions: "b"}},
		{"no metadata", &Skill{Name: "bare", Description: "Use when bare", Instructions: "b"}},
		{"license and compatibility", &Skill{
			Name:          "licensed",
			Description:   "Use when licensed",
			Instructions:  "b",
			License:       "MIT",
			Compatibility: "mocode >= 1.0",
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data, err := Render(tt.skill)
			require.NoError(t, err)
			require.True(t, strings.HasPrefix(string(data), "---\n"), "frontmatter must open with a fence")

			got, err := ParseContent(data)
			require.NoError(t, err, "rendered output must parse:\n%s", data)

			assert.Equal(t, tt.skill.Name, got.Name)
			assert.Equal(t, tt.skill.Description, got.Description)
			assert.Equal(t, strings.TrimSpace(tt.skill.Instructions), got.Instructions)
			assert.Equal(t, tt.skill.License, got.License)
			assert.Equal(t, tt.skill.Compatibility, got.Compatibility)
			assert.Equal(t, tt.skill.Metadata, got.Metadata)
		})
	}
}

// TestWriteFile_CreateWritesDiscoverableSkill covers the happy path end to end:
// the file lands at <root>/<name>/SKILL.md and discovery finds it.
func TestWriteFile_CreateWritesDiscoverableSkill(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	res, err := WriteFile(root, sampleSkill(), fixedNow())
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(root, "deploy-runbook", SkillFileName), res.Path)
	assert.False(t, res.Overwrote)
	assert.Empty(t, res.Backup)

	found := Discover([]string{root})
	require.Len(t, found, 1)
	assert.Equal(t, "deploy-runbook", found[0].Name)
	assert.Equal(t, OriginLearn, found[0].Metadata[MetaOrigin])
}

// TestWriteFile_BackupIsNotDiscovered is the subtle one: backups live under the
// skills root, so if the copy were named SKILL.md it would be loaded as a live
// skill and shadow the real one.
func TestWriteFile_BackupIsNotDiscovered(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	_, err := WriteFile(root, sampleSkill(), fixedNow())
	require.NoError(t, err)

	updated := sampleSkill()
	updated.Instructions = "# Deploy\n\n1. Run `task dev:ship`.\n2. Verify the version.\n"
	updated.Metadata[MetaRevision] = "2"

	res, err := WriteFile(root, updated, fixedNow().Add(time.Hour))
	require.NoError(t, err)
	require.True(t, res.Overwrote)
	require.NotEmpty(t, res.Backup)

	// The backup must exist and hold the previous body…
	backup, err := os.ReadFile(res.Backup)
	require.NoError(t, err)
	assert.Contains(t, string(backup), "task dev:ship")

	// …but discovery must see exactly one skill: the updated one.
	found := Discover([]string{root})
	require.Len(t, found, 1, "backup copies must not be discovered as skills")
	assert.Contains(t, found[0].Instructions, "Verify the version")
}

// TestWriteFile_SecondBackupSameSecondDoesNotClobber guards the timestamp
// collision: two rewrites within the same second must both be recoverable.
func TestWriteFile_SecondBackupSameSecondDoesNotClobber(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	_, err := WriteFile(root, sampleSkill(), fixedNow())
	require.NoError(t, err)

	second := sampleSkill()
	second.Instructions = "second body"
	res2, err := WriteFile(root, second, fixedNow())
	require.NoError(t, err)

	third := sampleSkill()
	third.Instructions = "third body"
	res3, err := WriteFile(root, third, fixedNow())
	require.NoError(t, err)

	assert.NotEqual(t, res2.Backup, res3.Backup, "backups in the same second must not overwrite each other")

	first, err := os.ReadFile(res3.Backup)
	require.NoError(t, err)
	assert.Contains(t, string(first), "second body", "the newest backup holds the immediately previous version")
}

// TestWriteFile_RejectsInvalidSkill ensures nothing reaches disk unless it
// passes the same validation discovery applies.
func TestWriteFile_RejectsInvalidSkill(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		skill *Skill
	}{
		{"no name", &Skill{Description: "Use when x", Instructions: "b"}},
		{"bad name", &Skill{Name: "Bad_Name", Description: "Use when x", Instructions: "b"}},
		{"no description", &Skill{Name: "ok-name", Instructions: "b"}},
		{"description too long", &Skill{Name: "ok-name", Description: strings.Repeat("x", MaxDescriptionLength+1), Instructions: "b"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			_, err := WriteFile(root, tt.skill, fixedNow())
			require.Error(t, err)

			entries, readErr := os.ReadDir(root)
			require.NoError(t, readErr)
			assert.Empty(t, entries, "a rejected skill must leave nothing behind")
		})
	}
}

// TestWriteFile_RejectsEmptyRoot prevents a nil/empty root from silently
// resolving to the process working directory.
func TestWriteFile_RejectsEmptyRoot(t *testing.T) {
	t.Parallel()

	_, err := WriteFile("   ", sampleSkill(), fixedNow())
	require.Error(t, err)

	_, err = WriteFile(t.TempDir(), nil, fixedNow())
	require.Error(t, err)

	_, err = WriteFile(t.TempDir(), &Skill{}, fixedNow())
	require.Error(t, err)
}

// TestRender_Deterministic keeps diffs on learned skills reviewable: rewriting
// the same skill must produce byte-identical output.
func TestRender_Deterministic(t *testing.T) {
	t.Parallel()

	s := sampleSkill()
	s.Metadata["zzz"] = "last"
	s.Metadata["aaa"] = "first"

	first, err := Render(s)
	require.NoError(t, err)
	second, err := Render(s)
	require.NoError(t, err)

	assert.Equal(t, string(first), string(second))
}
