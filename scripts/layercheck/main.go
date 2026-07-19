// Command layercheck verifies the internal/ layer boundaries documented in
// internal/README.md:
//
//	util(0) <- domain(1) <- store(2) <- core(3) <- transport/integration/ui(4)
//
// A lower layer must not import a higher layer. It parses import paths via
// `go list` and is independent of golangci-lint version, so it works on any
// toolchain. Run: go run ./scripts/layercheck
//
// Exit code is non-zero if any boundary is violated. Known pre-existing
// exceptions (pending fixes) are documented in
// docs/dev-notes/structure-governance-baseline.md.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// layerOf returns the architectural layer number for an internal package path,
// or -1 if it is not a layered internal package.
func layerOf(importPath string) int {
	const prefix = "github.com/nextsko/mocode-agent/internal/"
	if !strings.HasPrefix(importPath, prefix) {
		return -1
	}
	rest := strings.TrimPrefix(importPath, prefix)
	container := rest
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		container = rest[:i]
	}
	switch container {
	case "util":
		return 0
	case "domain":
		return 1
	case "store":
		return 2
	case "core":
		return 3
	case "transport", "integration", "ui":
		return 4
	default:
		return -1
	}
}

type violation struct {
	pkg     string
	imports string
	from    int
	to      int
}

func main() {
	out, err := exec.Command("go", "list", "./internal/...").Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "layercheck: go list failed: %v\n", err)
		os.Exit(2)
	}
	pkgs := strings.Fields(string(out))
	sort.Strings(pkgs)

	var violations []violation
	for _, pkg := range pkgs {
		fromLayer := layerOf(pkg)
		if fromLayer < 0 {
			continue
		}
		jsonOut, err := exec.Command("go", "list", "-json", pkg).Output()
		if err != nil {
			continue
		}
		for _, imp := range extractImports(string(jsonOut)) {
			toLayer := layerOf(imp)
			if toLayer < 0 {
				continue
			}
			if toLayer > fromLayer {
				violations = append(violations, violation{pkg: pkg, imports: imp, from: fromLayer, to: toLayer})
			}
		}
	}

	if len(violations) == 0 {
		fmt.Println("layercheck: no upward dependency violations.")
		return
	}
	fmt.Printf("layercheck: %d upward dependency violation(s):\n", len(violations))
	for _, v := range violations {
		fmt.Printf("  L%d -> L%d  %s imports %s\n", v.from, v.to, v.pkg, v.imports)
	}
	fmt.Println("\nSee docs/dev-notes/structure-governance-baseline.md for known exceptions.")
	os.Exit(1)
}

// extractImports pulls the Imports array out of `go list -json` output by
// string-matching, avoiding an encoding/json dependency for a tiny check.
func extractImports(json string) []string {
	var imports []string
	const marker = `"Imports": [`
	idx := strings.Index(json, marker)
	if idx < 0 {
		return nil
	}
	rest := json[idx+len(marker):]
	end := strings.Index(rest, "]")
	if end < 0 {
		return nil
	}
	for _, line := range strings.Split(rest[:end], ",") {
		line = strings.TrimSpace(line)
		line = strings.Trim(line, `"`)
		if line != "" {
			imports = append(imports, line)
		}
	}
	return imports
}
