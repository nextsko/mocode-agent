package fsext

import (
	"fmt"
	"os"
	"strings"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
)

// Expand is a wrapper around [expand.Literal]. It will escape the input
// string, expand any shell symbols (such as '~') and resolve any environment
// variables.
func Expand(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	p := syntax.NewParser()
	word, err := p.Document(strings.NewReader(s))
	if err != nil {
		return "", fmt.Errorf("parse shell expression %q: %w", s, err)
	}
	cfg := &expand.Config{
		Env:      expand.FuncEnviron(os.Getenv),
		ReadDir2: os.ReadDir,
		GlobStar: true,
	}
	lit, err := expand.Literal(cfg, word)
	if err != nil {
		return "", fmt.Errorf("expand shell expression %q: %w", s, err)
	}
	return lit, nil
}
