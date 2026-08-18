// Command layercheck verifies the internal/ layer boundaries documented in
// internal/README.md:
//
//	util(0) <- domain(1) <- store(2) <- core(3) <- transport/integration/ui(4)
//
// A lower layer must not import a higher layer. It parses import paths via
// `go list` and is independent of golangci-lint version, so it works on any
// toolchain. Run: go run ./scripts/layercheck
//
// In SysML terms this command is the constraint verifier for the package
// model: layerRule is the structural constraint, packageRules are the
// per-interface-block constraint set (e.g. the nethttp port must stay free of
// LLM-runtime and config coupling). Exit code is non-zero if any boundary is
// violated. Known pre-existing exceptions (pending fixes) are documented in
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

// packageRule bans specific imports inside one package subtree. Unlike the
// layer rule these constraints are opt-in per block, so the model can be
// tightened incrementally without failing the build on acknowledged debt.
type packageRule struct {
	pkg       string   // package the rule applies to (exact or subtree prefix)
	forbidden []string // import prefixes banned inside that subtree
}

// packageRules mirrors the constraints published in
// internal/core/tools/doc.go ("Enforced package constraints"). Keep the two
// lists in sync.
var packageRules = []packageRule{
	{
		pkg: "github.com/nextsko/mocode-agent/internal/core/tools/external/nethttp",
		forbidden: []string{
			"charm.land/fantasy",
			"charm.land/catwalk",
			"github.com/nextsko/mocode-agent/internal/core/agent",
			"github.com/nextsko/mocode-agent/internal/core/config",
		},
	},
	{
		pkg: "github.com/nextsko/mocode-agent/internal/core/tools/external/plugins/netcommon",
		forbidden: []string{
			"charm.land/fantasy",
			"charm.land/catwalk",
			"github.com/nextsko/mocode-agent/internal/core/agent",
		},
	},
}

// bannedBy reports the first forbidden import matching a rule for pkgPath.
func bannedBy(pkgPath string, imports []string) (rule packageRule, hit string, banned bool) {
	for _, r := range packageRules {
		if pkgPath != r.pkg && !strings.HasPrefix(pkgPath, r.pkg+"/") {
			continue
		}
		for _, imp := range imports {
			for _, bad := range r.forbidden {
				if imp == bad || strings.HasPrefix(imp, bad+"/") {
					return r, imp, true
				}
			}
		}
	}
	return packageRule{}, "", false
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
		imports := extractImports(string(jsonOut))
		for _, imp := range imports {
			toLayer := layerOf(imp)
			if toLayer < 0 {
				continue
			}
			if toLayer > fromLayer {
				violations = append(violations, violation{pkg: pkg, imports: imp, from: fromLayer, to: toLayer})
			}
		}
		if rule, imp, banned := bannedBy(pkg, imports); banned {
			fmt.Printf("layercheck: package constraint violated: %s imports %s (banned in %s)\n", pkg, imp, rule.pkg)
			os.Exit(1)
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
