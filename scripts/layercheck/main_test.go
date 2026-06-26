package main

import "testing"

func TestLayerOf(t *testing.T) {
	cases := []struct {
		path string
		want int
	}{
		// Layered internal packages map to their container layer.
		{"github.com/package-register/mocode/internal/util/csync", 0},
		{"github.com/package-register/mocode/internal/util/fsext", 0},
		{"github.com/package-register/mocode/internal/domain/session", 1},
		{"github.com/package-register/mocode/internal/store", 2},
		{"github.com/package-register/mocode/internal/core/agent", 3},
		{"github.com/package-register/mocode/internal/core/app", 3},
		{"github.com/package-register/mocode/internal/transport/cmd", 4},
		{"github.com/package-register/mocode/internal/integration/wechat", 4},
		{"github.com/package-register/mocode/internal/ui/model", 4},
		// Non-internal and unknown containers are not layered.
		{"github.com/package-register/mocode/cmd", -1},
		{"github.com/stretchr/testify/require", -1},
		{"github.com/package-register/mocode/internal/somethingnew", -1},
	}
	for _, c := range cases {
		got := layerOf(c.path)
		if got != c.want {
			t.Errorf("layerOf(%q) = %d, want %d", c.path, got, c.want)
		}
	}
}

func TestExtractImports(t *testing.T) {
	// A realistic go list -json fragment.
	json := `{
	"Dir": "/x",
	"ImportPath": "example.com/p",
	"Imports": ["fmt", "github.com/package-register/mocode/internal/util/csync"]
}`
	got := extractImports(json)
	if len(got) != 2 {
		t.Fatalf("expected 2 imports, got %d: %v", len(got), got)
	}
	if got[1] != "github.com/package-register/mocode/internal/util/csync" {
		t.Errorf("unexpected second import: %q", got[1])
	}
}

func TestExtractImportsNoImports(t *testing.T) {
	// Packages with no imports have no Imports array.
	got := extractImports(`{"ImportPath":"x"}`)
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}
