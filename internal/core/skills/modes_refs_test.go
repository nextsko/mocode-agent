package skills

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestModeSkillReferencesExist guards against dead references: every skill
// named in a mode's 「主导技能」list must be a real builtin skill. This keeps
// roles honest — a role that points at a missing skill is dead weight.
func TestModeSkillReferencesExist(t *testing.T) {
	t.Parallel()

	builtin := map[string]bool{}
	for _, s := range DiscoverBuiltin() {
		builtin[s.Name] = true
	}

	dir := filepath.Join("..", "config", "templates", "modes")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	bullet := regexp.MustCompile("^- `([a-z0-9-]+)`")
	checked := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, e.Name()))
		require.NoError(t, err)

		inSection := false
		for _, line := range strings.Split(string(content), "\n") {
			line = strings.TrimRight(line, " \t")
			if strings.HasPrefix(line, "## 主导技能") {
				inSection = true
				continue
			}
			if inSection && strings.HasPrefix(line, "## ") {
				inSection = false
			}
			if !inSection {
				continue
			}
			if m := bullet.FindStringSubmatch(line); m != nil {
				checked++
				require.True(t, builtin[m[1]], "mode %s references missing skill %q", e.Name(), m[1])
			}
		}
	}
	require.Greater(t, checked, 0, "expected to find skill references in mode files")
}
