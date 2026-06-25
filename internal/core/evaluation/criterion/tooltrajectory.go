package criterion

import (
	"fmt"
	"strings"

	"github.com/package-register/mocode/internal/core/evaluation/evalset"
	"github.com/package-register/mocode/internal/domain/session/message"
)

// Score evaluates the ToolTrajectory criterion against the tool calls actually
// made in a trace, returning a normalized score in [0, 1].
//
// Exact takes precedence over IgnoreOrder when both are set:
//   - Exact: 1.0 only if actual names equal the expected sequence in order and
//     any non-empty ExpectedToolCall.Input is a substring of the actual input.
//   - IgnoreOrder: 1.0 if every expected name appears at least once.
//
// With no expected tools, the criterion is satisfied vacuously (1.0).
func (t *ToolTrajectory) Score(actual []message.ToolCall, expected []evalset.ExpectedToolCall) (float64, string, error) {
	if t == nil {
		return 1.0, "tool-trajectory: criterion disabled", nil
	}
	if len(expected) == 0 {
		return 1.0, "tool-trajectory: no expected tools, vacuously satisfied", nil
	}
	switch {
	case t.Exact:
		return scoreExact(actual, expected)
	default:
		// IgnoreOrder or unset both fall back to a presence (subset) check.
		return scoreSubset(actual, expected)
	}
}

func scoreExact(actual []message.ToolCall, expected []evalset.ExpectedToolCall) (float64, string, error) {
	if len(actual) != len(expected) {
		return 0.0, fmt.Sprintf("tool-trajectory(exact): length mismatch, expected %d got %d", len(expected), len(actual)), nil
	}
	for i, exp := range expected {
		act := actual[i]
		if act.Name != exp.Name {
			return 0.0, fmt.Sprintf("tool-trajectory(exact): step %d expected %q got %q", i, exp.Name, act.Name), nil
		}
		if exp.Input != "" && !strings.Contains(act.Input, exp.Input) {
			return 0.0, fmt.Sprintf("tool-trajectory(exact): step %d (%s) input missing %q", i, exp.Name, exp.Input), nil
		}
	}
	return 1.0, "tool-trajectory(exact): sequence matched", nil
}

func scoreSubset(actual []message.ToolCall, expected []evalset.ExpectedToolCall) (float64, string, error) {
	seen := make(map[string]bool, len(actual))
	for _, call := range actual {
		seen[call.Name] = true
	}
	for _, exp := range expected {
		if !seen[exp.Name] {
			return 0.0, fmt.Sprintf("tool-trajectory(subset): expected tool %q not called", exp.Name), nil
		}
	}
	return 1.0, "tool-trajectory(subset): all expected tools present", nil
}
