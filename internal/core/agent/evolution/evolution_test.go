package evolution

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCreateAndListPatch(t *testing.T) {
	tmp := t.TempDir()
	store, err := NewPatchStore(tmp)
	if err != nil {
		t.Fatal(err)
	}

	p := Patch{
		ID:          "patch-test-001",
		Kind:        KindRule,
		Title:       "Strengthen file operation constraints",
		Description: "Agent tried to edit files outside working dir. Add path validation rule.",
		Priority:    1,
		Source:      "sess-001",
		CreatedAt:   time.Now(),
	}
	patchDir, err := store.CreatePatch(p)
	if err != nil {
		t.Fatal(err)
	}

	// Write a rule file into the patch.
	err = store.WriteFile(patchDir, "rules", "path-guard.md",
		"## Rule: Path Validation\n\nMUST: Verify file path is within working directory before writing.",
	)
	if err != nil {
		t.Fatal(err)
	}

	// List patches.
	patches, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(patches) != 1 {
		t.Fatalf("expected 1 patch, got %d", len(patches))
	}
	if patches[0].Title != p.Title {
		t.Errorf("wrong title: %s", patches[0].Title)
	}

	// Check unapplied.
	unapplied, err := store.ListUnapplied()
	if err != nil {
		t.Fatal(err)
	}
	if len(unapplied) != 1 {
		t.Fatalf("expected 1 unapplied, got %d", len(unapplied))
	}
}

func TestMarkApplied(t *testing.T) {
	tmp := t.TempDir()
	store, _ := NewPatchStore(tmp)
	p := Patch{ID: "p1", Kind: KindMemory, Title: "Test", Priority: 3, CreatedAt: time.Now()}
	store.CreatePatch(p)

	err := store.MarkApplied("p1")
	if err != nil {
		t.Fatal(err)
	}

	unapplied, _ := store.ListUnapplied()
	if len(unapplied) != 0 {
		t.Errorf("expected 0 unapplied after mark, got %d", len(unapplied))
	}
}

func TestBuildContext_LoadsRules(t *testing.T) {
	tmp := t.TempDir()
	store, _ := NewPatchStore(tmp)

	p := Patch{ID: "p-rule-1", Kind: KindRule, Title: "Fix A", Description: "desc A", Priority: 1, CreatedAt: time.Now()}
	patchDir, _ := store.CreatePatch(p)
	store.WriteFile(patchDir, "rules", "guard.md", "MUST: Always check foo before bar.")

	loader := NewPatchLoader(store)
	ctx, err := loader.BuildContext()
	if err != nil {
		t.Fatal(err)
	}
	if ctx == "" {
		t.Error("expected non-empty context with rules")
	}

	// Verify mark-applied works.
	if err := loader.ApplyAll(); err != nil {
		t.Fatal(err)
	}
	// Now context should be empty since all applied.
	ctx2, _ := loader.BuildContext()
	if ctx2 != "" {
		t.Errorf("expected empty context after ApplyAll, got: %s", ctx2)
	}
}

func TestEmptyStore(t *testing.T) {
	tmp := t.TempDir()
	store, _ := NewPatchStore(tmp)
	patches, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(patches) != 0 {
		t.Errorf("expected empty list, got %d", len(patches))
	}

	loader := NewPatchLoader(store)
	ctx, _ := loader.BuildContext()
	if ctx != "" {
		t.Errorf("expected empty context, got: %s", ctx)
	}
}

func TestPatchDirectory_FileExists(t *testing.T) {
	tmp := t.TempDir()
	store, _ := NewPatchStore(tmp)
	p := Patch{ID: "with-files", Kind: KindSkill, Title: "New Skill", Priority: 2, CreatedAt: time.Now()}
	patchDir, _ := store.CreatePatch(p)

	// Write a skill file.
	store.WriteFile(patchDir, "skills", "my-skill.md", "# MySkill\n\nDoes things.")
	_, err := os.Stat(filepath.Join(patchDir, "skills", "my-skill.md"))
	if err != nil {
		t.Fatalf("skill file should exist: %v", err)
	}
}
