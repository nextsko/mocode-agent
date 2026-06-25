package shell

import (
	"bytes"
	"fmt"
	"strings"

	mvshell "mvdan.cc/sh/v3/shell"
	"mvdan.cc/sh/v3/syntax"
)

func validateCommandAgainstBlockers(command string, env []string, blockFuncs []BlockFunc) error {
	if len(blockFuncs) == 0 {
		return nil
	}

	file, err := syntax.NewParser().Parse(strings.NewReader(command), "")
	if err != nil {
		return fmt.Errorf("could not parse command: %w", err)
	}

	envLookup := make(map[string]string, len(env))
	for _, kv := range env {
		key, value, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		envLookup[key] = value
	}

	printer := syntax.NewPrinter()
	var blocked error
	syntax.Walk(file, func(node syntax.Node) bool {
		if blocked != nil {
			return false
		}

		call, ok := node.(*syntax.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}

		var buf bytes.Buffer
		for i, arg := range call.Args {
			if i > 0 {
				buf.WriteByte(' ')
			}
			if err := printer.Print(&buf, arg); err != nil {
				blocked = fmt.Errorf("print command: %w", err)
				return false
			}
		}

		fields, err := mvshell.Fields(buf.String(), func(name string) string {
			return envLookup[name]
		})
		if err != nil {
			blocked = fmt.Errorf("expand command: %w", err)
			return false
		}
		if len(fields) == 0 {
			return true
		}

		for _, blockFunc := range blockFuncs {
			if blockFunc(fields) {
				blocked = fmt.Errorf("command is not allowed for security reasons: %q", fields[0])
				return false
			}
		}
		return true
	})

	return blocked
}
