package criterion

import (
	"context"
	"strings"

	"github.com/nextsko/mocode-agent/internal/core/evaluation/evalset"
	"github.com/nextsko/mocode-agent/internal/domain/session/message"
)

// ErrNoCriteria signals that a Criterion has no enabled sub-criteria to evaluate.
type ErrNoCriteria struct{}

func (ErrNoCriteria) Error() string { return "criterion: no sub-criteria enabled" }

// Judger scores a trace against rubrics using an LLM.
// It is the seam where Phase 4 (LLMJudge) plugs in; implementations are injected
// by the evaluation service rather than imported here, keeping the criterion
// package free of model/provider dependencies.
type Judger interface {
	// Judge scores the given trace. The returned score is clamped to [0, 1].
	Judge(ctx context.Context, rubrics []string, trace []message.Message) (EvalResult, error)
}

// Eval scores the Criterion against a trace.
//
// cs carries case-level expectations (ExpectedTools, Rubrics, ExpectedText);
// trace is the full message.Service.List result for the inference session.
// When a Judger is provided and the LLMJudge sub-criterion is set, it scores the
// trace; otherwise LLMJudge is skipped.
//
// Multiple enabled sub-criteria are aggregated by taking the minimum score
// (the strictest sub-criterion wins); reasons are concatenated in order.
func (c *Criterion) Eval(ctx context.Context, cs *evalset.EvalCase, trace []message.Message, judger Judger) (EvalResult, error) {
	if c == nil || (c.FinalResponse == nil && c.ToolTrajectory == nil && c.LLMJudge == nil) {
		return EvalResult{}, ErrNoCriteria{}
	}
	finalText := FinalText(trace)
	toolCalls := ToolCalls(trace)

	var (
		scores  []float64
		reasons []string
	)
	if c.FinalResponse != nil {
		// Allow the case ExpectedText to override the criterion Expected for
		// convenience: a case can carry its own expected text without a bespoke
		// criterion instance per case.
		fr := *c.FinalResponse
		if cs != nil && cs.HasExpectedText() && fr.Expected == "" {
			fr.Expected = cs.ExpectedText
		}
		score, reason, err := fr.Score(finalText)
		if err != nil {
			return EvalResult{}, err
		}
		scores = append(scores, score)
		reasons = append(reasons, reason)
	}
	if c.ToolTrajectory != nil {
		expected := cs.ExpectedTools
		score, reason, err := c.ToolTrajectory.Score(toolCalls, expected)
		if err != nil {
			return EvalResult{}, err
		}
		scores = append(scores, score)
		reasons = append(reasons, reason)
	}
	if c.LLMJudge != nil && judger != nil {
		rubrics := c.LLMJudge.Rubrics
		if len(rubrics) == 0 && cs != nil {
			rubrics = cs.Rubrics
		}
		if len(rubrics) > 0 {
			res, err := judger.Judge(ctx, rubrics, trace)
			if err != nil {
				return EvalResult{}, err
			}
			scores = append(scores, res.Score)
			reasons = append(reasons, res.Reason)
		}
	}
	if len(scores) == 0 {
		return EvalResult{Score: 1.0, Reason: "criterion: only LLMJudge enabled but no judger provided, skipped"}, nil
	}
	min := scores[0]
	for _, s := range scores[1:] {
		if s < min {
			min = s
		}
	}
	return EvalResult{Score: min, Reason: strings.Join(reasons, "; ")}, nil
}

// FinalText extracts the agent final text response from a trace.
//
// It scans assistant messages in order and returns the text of the last one
// that carries non-whitespace TextContent. This matches mocode's convention
// that the terminal assistant message (finish reason end_turn) holds the final
// answer; intermediate assistant messages may contain only tool calls.
func FinalText(trace []message.Message) string {
	var last string
	for _, msg := range trace {
		if msg.Role != message.Assistant {
			continue
		}
		text := strings.TrimSpace(msg.Content().Text)
		if text != "" {
			last = msg.Content().Text
		}
	}
	return last
}

// ToolCalls extracts the ordered sequence of tool calls made by the agent.
//
// Assistant messages emit their ToolCall parts in order. Sub-agent calls are
// also recorded as assistant messages by the coordinator.
func ToolCalls(trace []message.Message) []message.ToolCall {
	var calls []message.ToolCall
	for _, msg := range trace {
		if msg.Role != message.Assistant {
			continue
		}
		calls = append(calls, msg.ToolCalls()...)
	}
	return calls
}
