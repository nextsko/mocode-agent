package gitopscommon

import (
	"testing"
)

func TestClassify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		path      string
		status    FileStatus
		wantType  CommitType
		wantScope string
	}{
		{
			name:      "new Go config file → feat/config",
			path:      "internal/config/app_name.go",
			status:    StatusUntracked,
			wantType:  TypeFeat,
			wantScope: "config",
		},
		{
			name:      "modified test file → test",
			path:      "internal/ui/model/quit_summary_test.go",
			status:    StatusModified,
			wantType:  TypeTest,
			wantScope: "ui",
		},
		{
			name:      "markdown docs → docs",
			path:      "README.md",
			status:    StatusModified,
			wantType:  TypeDocs,
			wantScope: "",
		},
		{
			name:      "deleted Go file → refactor",
			path:      "internal/ui/dialog/rollback.go",
			status:    StatusDeleted,
			wantType:  TypeRefactor,
			wantScope: "ui",
		},
		{
			name:      "go.mod → chore/deps",
			path:      "go.mod",
			status:    StatusModified,
			wantType:  TypeChore,
			wantScope: "deps",
		},
		{
			name:      "github CI → ci",
			path:      ".github/workflows/test.yml",
			status:    StatusModified,
			wantType:  TypeCI,
			wantScope: "ci",
		},
		{
			name:      "agent template → docs/agent",
			path:      "internal/agent/templates/coder.md.tpl",
			status:    StatusModified,
			wantType:  TypeDocs,
			wantScope: "agent",
		},
		{
			name:      "admin HTML → feat/admin",
			path:      "internal/admin/assets/index.html",
			status:    StatusModified,
			wantType:  TypeFeat,
			wantScope: "admin",
		},
		{
			name:      "notification native.go → refactor/ui",
			path:      "internal/ui/notification/native.go",
			status:    StatusModified,
			wantType:  TypeUI,
			wantScope: "ui",
		},
		{
			name:      "skills builtin → docs/skills",
			path:      "internal/skills/builtin/mocode-hooks/SKILL.md",
			status:    StatusUntracked,
			wantType:  TypeDocs,
			wantScope: "skills",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fc := FileChange{Path: tt.path, Status: tt.status}
			got := Classify(fc)
			if got.Type != tt.wantType {
				t.Errorf("type: got %s, want %s", got.Type, tt.wantType)
			}
			if got.Scope != tt.wantScope {
				t.Errorf("scope: got %s, want %s", got.Scope, tt.wantScope)
			}
		})
	}
}

func TestInferScope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		want string
	}{
		{"internal/config/config.go", "config"},
		{"internal/ui/model/ui.go", "ui"},
		{"internal/agent/tools/plugins/gitops/plugin.go", "tools"},
		{"internal/agent/agent.go", "agent"},
		{"internal/session/store.go", "session"},
		{"internal/hooks/runner.go", "hooks"},
		{"internal/skills/embed.go", "skills"},
		{"internal/wechat/sdk/client.go", "wechat"},
		{".github/SECURITY-FIX-PLAN.md", "ci"},
		{"go.mod", ""},
		{"README.md", ""},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			got := inferScope(tt.path)
			if got != tt.want {
				t.Errorf("inferScope(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestEmojiMap(t *testing.T) {
	t.Parallel()
	if emojiMap[TypeFeat] != "✨" {
		t.Errorf("feat emoji = %q, want ✨", emojiMap[TypeFeat])
	}
	if emojiMap[TypeFix] != "🐛" {
		t.Errorf("fix emoji = %q, want 🐛", emojiMap[TypeFix])
	}
}

func TestSplitLargeGroup(t *testing.T) {
	t.Parallel()

	// Small group should not be split
	small := &CommitGroup{
		Type:  "feat",
		Scope: "ui",
		Files: []string{"a.go", "b.go", "c.go"},
	}
	split := splitLargeGroup(small)
	if len(split) != 1 {
		t.Errorf("small group split into %d, want 1", len(split))
	}

	// Large group should be split
	large := &CommitGroup{
		Type:  "refactor",
		Scope: "ui",
		Files: []string{
			"internal/ui/a.go", "internal/ui/b.go", "internal/ui/c.go",
			"internal/ui/d.go", "internal/ui/e.go", "internal/ui/f.go",
			"internal/ui/g.go", "internal/ui/h.go", "internal/ui/i.go",
			"internal/ui/j.go",
		},
	}
	split = splitLargeGroup(large)
	if len(split) < 2 {
		t.Errorf("large group split into %d, want >= 2", len(split))
	}

	totalFiles := 0
	for _, g := range split {
		totalFiles += len(g.Files)
	}
	if totalFiles != 10 {
		t.Errorf("total files after split = %d, want 10", totalFiles)
	}
}
