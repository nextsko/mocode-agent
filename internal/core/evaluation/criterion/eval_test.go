package criterion

import (
	"context"
	"errors"
	"testing"

	"github.com/nextsko/mocode-agent/internal/core/evaluation/evalset"
	"github.com/nextsko/mocode-agent/internal/domain/session/message"
)

func assistant(text string, calls ...message.ToolCall) message.Message {
	parts := []message.ContentPart{}
	if text != "" {
		parts = append(parts, message.TextContent{Text: text})
	}
	for _, c := range calls {
		parts = append(parts, c)
	}
	return message.Message{Role: message.Assistant, Parts: parts}
}

func user(text string) message.Message {
	return message.Message{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: text}}}
}

func TestFinalResponseScore(t *testing.T) {
	tests := []struct {
		name    string
		fr      FinalResponse
		actual  string
		want    float64
		wantErr bool
	}{
		{"exact match", FinalResponse{Mode: MatchExact, Expected: "Hello"}, "Hello", 1.0, false},
		{"exact mismatch", FinalResponse{Mode: MatchExact, Expected: "Hello"}, "World", 0.0, false},
		{"contains present", FinalResponse{Mode: MatchContains, Expected: "ell"}, "Hello", 1.0, false},
		{"contains absent", FinalResponse{Mode: MatchContains, Expected: "xyz"}, "Hello", 0.0, false},
		{"regex match", FinalResponse{Mode: MatchRegex, Expected: "^H.+o$"}, "Hello", 1.0, false},
		{"regex no match", FinalResponse{Mode: MatchRegex, Expected: "^X"}, "Hello", 0.0, false},
		{"regex invalid", FinalResponse{Mode: MatchRegex, Expected: "("}, "Hello", 0.0, true},
		{"case insensitive", FinalResponse{Mode: MatchExact, Expected: "hello", CaseInsensitive: true}, "HELLO", 1.0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score, _, err := tt.fr.Score(tt.actual)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if score != tt.want {
				t.Fatalf("score = %v, want %v", score, tt.want)
			}
		})
	}
}

func TestToolTrajectoryScore(t *testing.T) {
	grep := message.ToolCall{Name: "grep"}
	view := message.ToolCall{Name: "view"}
	expected := []evalset.ExpectedToolCall{{Name: "grep"}, {Name: "view"}}

	t.Run("exact match", func(t *testing.T) {
		tt := ToolTrajectory{Exact: true}
		score, _, err := tt.Score([]message.ToolCall{grep, view}, expected)
		mustNoErr(t, err)
		mustScore(t, score, 1.0)
	})
	t.Run("exact wrong order", func(t *testing.T) {
		tt := ToolTrajectory{Exact: true}
		score, _, _ := tt.Score([]message.ToolCall{view, grep}, expected)
		mustScore(t, score, 0.0)
	})
	t.Run("exact input missing", func(t *testing.T) {
		exp := []evalset.ExpectedToolCall{{Name: "grep", Input: "TODO"}}
		call := message.ToolCall{Name: "grep", Input: "no match here"}
		tt := ToolTrajectory{Exact: true}
		score, _, _ := tt.Score([]message.ToolCall{call}, exp)
		mustScore(t, score, 0.0)
	})
	t.Run("ignore order match", func(t *testing.T) {
		tt := ToolTrajectory{IgnoreOrder: true}
		score, _, err := tt.Score([]message.ToolCall{view, grep}, expected)
		mustNoErr(t, err)
		mustScore(t, score, 1.0)
	})
	t.Run("ignore order missing", func(t *testing.T) {
		tt := ToolTrajectory{IgnoreOrder: true}
		score, _, _ := tt.Score([]message.ToolCall{grep}, expected)
		mustScore(t, score, 0.0)
	})
	t.Run("no expected vacuous", func(t *testing.T) {
		tt := ToolTrajectory{Exact: true}
		score, _, err := tt.Score(nil, nil)
		mustNoErr(t, err)
		mustScore(t, score, 1.0)
	})
}

type fakeJudger struct {
	score float64
	err   error
}

func (f fakeJudger) Judge(ctx context.Context, rubrics []string, trace []message.Message) (EvalResult, error) {
	if f.err != nil {
		return EvalResult{}, f.err
	}
	return EvalResult{Score: f.score, Reason: "fake judge"}, nil
}

func TestCriterionEval(t *testing.T) {
	cs := &evalset.EvalCase{
		ID:            "c1",
		UserContent:   "say hello",
		ExpectedText:  "Hello World",
		ExpectedTools: []evalset.ExpectedToolCall{{Name: "grep"}},
	}
	trace := []message.Message{
		user("say hello"),
		assistant("", message.ToolCall{Name: "grep"}),
		assistant("Hello World"),
	}

	t.Run("final response from case expected text", func(t *testing.T) {
		c := &Criterion{FinalResponse: &FinalResponse{Mode: MatchExact}}
		res, err := c.Eval(context.Background(), cs, trace, nil)
		mustNoErr(t, err)
		mustScore(t, res.Score, 1.0)
	})
	t.Run("final response mismatch", func(t *testing.T) {
		c := &Criterion{FinalResponse: &FinalResponse{Mode: MatchExact, Expected: "Goodbye"}}
		res, err := c.Eval(context.Background(), cs, trace, nil)
		mustNoErr(t, err)
		mustScore(t, res.Score, 0.0)
	})
	t.Run("aggregate min across final and trajectory", func(t *testing.T) {
		c := &Criterion{
			FinalResponse:  &FinalResponse{Mode: MatchExact, Expected: "Hello World"},
			ToolTrajectory: &ToolTrajectory{Exact: true},
		}
		// grep is present, so trajectory = 1.0; final matches = 1.0; min = 1.0
		res, err := c.Eval(context.Background(), cs, trace, nil)
		mustNoErr(t, err)
		mustScore(t, res.Score, 1.0)
		// Now break trajectory: expect a missing tool.
		cs2 := &evalset.EvalCase{ExpectedText: "Hello World", ExpectedTools: []evalset.ExpectedToolCall{{Name: "missing"}}}
		res2, err := c.Eval(context.Background(), cs2, trace, nil)
		mustNoErr(t, err)
		mustScore(t, res2.Score, 0.0)
	})
	t.Run("no criteria errors", func(t *testing.T) {
		_, err := (&Criterion{}).Eval(context.Background(), cs, trace, nil)
		if !errors.Is(err, ErrNoCriteria{}) {
			t.Fatalf("expected ErrNoCriteria, got %v", err)
		}
	})
	t.Run("llm judge without judger skipped", func(t *testing.T) {
		c := &Criterion{LLMJudge: &LLMJudge{Rubrics: []string{"be helpful"}}}
		res, err := c.Eval(context.Background(), cs, trace, nil)
		mustNoErr(t, err)
		mustScore(t, res.Score, 1.0)
	})
	t.Run("llm judge with judger", func(t *testing.T) {
		c := &Criterion{LLMJudge: &LLMJudge{Rubrics: []string{"be helpful"}}}
		res, err := c.Eval(context.Background(), cs, trace, fakeJudger{score: 0.6})
		mustNoErr(t, err)
		mustScore(t, res.Score, 0.6)
	})
}

func TestFinalText(t *testing.T) {
	trace := []message.Message{
		assistant("thinking text"),
		assistant(""),
		assistant("final answer"),
	}
	if got := FinalText(trace); got != "final answer" {
		t.Fatalf("FinalText = %q, want %q", got, "final answer")
	}
}

func mustNoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func mustScore(t *testing.T, got, want float64) {
	t.Helper()
	if got != want {
		t.Fatalf("score = %v, want %v", got, want)
	}
}
