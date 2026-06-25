package candidate

import (
	"context"
	"fmt"

	"github.com/package-register/mocode/internal/evaluation/criterion"
	"github.com/package-register/mocode/internal/evaluation/evalset"
)

// EvalSelector scores candidates with a criterion and selects the highest
// scoring one. It is the bridge between the evaluation system and online
// candidate selection: the same criterion that judges an eval case offline
// picks the best response here.
//
// Scoring uses criterion.Criterion.Eval against each candidate's Trace, with
// the provided eval case supplying ExpectedTools/ExpectedText/Rubrics. Failed
// candidates (Err != nil) are skipped and never selected.
type EvalSelector struct {
	// Criterion scores each candidate. Required.
	Criterion *criterion.Criterion
	// Case carries case-level expectations forwarded to Criterion.Eval. May be
	// nil; when nil, only the criterion's own expectations are used.
	Case *evalset.EvalCase
	// Judger scores LLMJudge criteria. May be nil; LLMJudge is then skipped.
	Judger criterion.Judger
}

// Compile-time assertion that EvalSelector satisfies Selector.
var _ Selector = (*EvalSelector)(nil)

// Select returns the 0-based index of the highest-scoring viable candidate.
//
// When scores tie, the earliest candidate wins (stable). If no viable
// candidate exists, Select returns an error.
func (s *EvalSelector) Select(ctx context.Context, candidates []Result) (int, error) {
	if s.Criterion == nil {
		return -1, fmt.Errorf("evalselector: criterion is required")
	}
	bestIdx := -1
	var bestScore float64
	for i, c := range candidates {
		if c.Err != nil {
			continue
		}
		res, err := s.Criterion.Eval(ctx, s.Case, c.Trace, s.Judger)
		if err != nil {
			// A scoring error disqualifies the candidate but does not abort
			// selection of the remaining candidates.
			continue
		}
		if bestIdx == -1 || res.Score > bestScore {
			bestIdx = i
			bestScore = res.Score
		}
	}
	if bestIdx == -1 {
		return -1, fmt.Errorf("evalselector: no viable candidate to score")
	}
	return bestIdx, nil
}
