// Package criterion defines scoring criteria for evaluation.
//
// A Criterion bundles up to three independent, optional sub-criteria:
//   - FinalResponse: asserts properties of the agent final text response.
//   - ToolTrajectory: asserts properties of the sequence of tool calls.
//   - LLMJudge: scores the response using another LLM against rubrics.
//
// Any nil sub-field is skipped during evaluation.
package criterion

// MatchMode controls how a FinalResponse criterion compares text.
type MatchMode string

const (
	// MatchExact requires the actual text to equal the expected text.
	MatchExact MatchMode = "exact"
	// MatchContains requires the expected text to appear as a substring.
	MatchContains MatchMode = "contains"
	// MatchRegex requires the actual text to match a regular expression.
	MatchRegex MatchMode = "regex"
)

// FinalResponse asserts properties of the agent final text response.
type FinalResponse struct {
	// Mode selects the comparison strategy.
	Mode MatchMode
	// Expected is the expected text (exact/contains) or pattern (regex).
	Expected string
	// CaseInsensitive relaxes the comparison to ignore case.
	CaseInsensitive bool
}

// ToolTrajectory asserts properties of the sequence of tool calls.
type ToolTrajectory struct {
	// Exact enforces that actual tool names match the expected sequence, in order.
	// Mutually exclusive with IgnoreOrder; Exact takes precedence when both are set.
	Exact bool
	// IgnoreOrder checks that all expected tool names appear, in any order.
	IgnoreOrder bool
}

// LLMJudge scores the response using another LLM against rubrics.
// The judge model is injected by the caller (see evaluation.LLMJudgeOption).
type LLMJudge struct {
	// Rubrics are judge-readable scoring criteria. When non-empty these override
	// the case-level Rubrics for scoring.
	Rubrics []string
}

// Criterion bundles optional sub-criteria. A nil field is skipped.
// When multiple sub-criteria are set, the aggregate score is the minimum of the
// individual scores (the strictest sub-criterion wins).
type Criterion struct {
	FinalResponse  *FinalResponse
	ToolTrajectory *ToolTrajectory
	LLMJudge       *LLMJudge
}

// EvalResult is the scored outcome of evaluating one Criterion against a trace.
type EvalResult struct {
	// Score is a normalized score in [0, 1].
	Score float64
	// Reason explains the score, e.g. why a match failed.
	Reason string
}
