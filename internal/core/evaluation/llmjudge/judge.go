// Package llmjudge implements criterion.Judger using a fantasy.LanguageModel.
//
// It reuses mocode's existing provider construction path: the caller builds a
// fantasy.LanguageModel (e.g. via provider.LanguageModel) and injects it here,
// so the judge shares the same provider/credentials as the configured models
// without depending on coordinator internals.
package llmjudge

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"charm.land/fantasy"

	"github.com/nextsko/mocode-agent/internal/core/evaluation/criterion"
	"github.com/nextsko/mocode-agent/internal/domain/session/message"
)

// Judge scores a trace against rubrics using an LLM.
//
// The model is a fully-constructed fantasy.LanguageModel (caller decides which
// provider/model serves as judge). MaxOutputTokens defaults to 1024; set it to
// tune the judge budget.
type Judge struct {
	Model           fantasy.LanguageModel
	MaxOutputTokens int64
}

// Compile-time assertion that Judge satisfies criterion.Judger.
var _ criterion.Judger = (*Judge)(nil)

// Judge formats rubrics and the trace into a single scoring prompt, runs the
// model, and parses a numeric score from the response.
//
// The model is asked to return exactly one line of the form "SCORE: <0..100>".
// The returned score is normalized to [0, 1]. A parse failure yields score 0
// with the raw text in the reason, so a flaky judge never silently passes a case.
func (j *Judge) Judge(ctx context.Context, rubrics []string, trace []message.Message) (criterion.EvalResult, error) {
	if j.Model == nil {
		return criterion.EvalResult{}, fmt.Errorf("llmjudge: model is required")
	}
	prompt := buildJudgePrompt(rubrics, trace)
	maxTokens := j.MaxOutputTokens
	if maxTokens == 0 {
		maxTokens = 1024
	}
	agent := fantasy.NewAgent(j.Model)
	res, err := agent.Generate(ctx, fantasy.AgentCall{
		Prompt:          prompt,
		MaxOutputTokens: &maxTokens,
	})
	if err != nil {
		return criterion.EvalResult{}, fmt.Errorf("llmjudge: generate: %w", err)
	}
	text := strings.TrimSpace(res.Response.Content.Text())
	score, parseErr := parseScore(text)
	reason := text
	if parseErr != nil {
		score = 0
		reason = "llmjudge: could not parse score from response: " + text
	}
	return criterion.EvalResult{Score: score, Reason: reason}, nil
}

// parseScore extracts the judge score and normalizes a 0..100 value to [0, 1].
// It tolerates surrounding text and "SCORE:" markers.
func parseScore(text string) (float64, error) {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Strip a leading "SCORE:" marker if present.
		if idx := strings.Index(strings.ToUpper(line), "SCORE:"); idx >= 0 {
			line = strings.TrimSpace(line[idx+len("SCORE:"):])
		}
		// Try the whole remaining line as a number.
		if f, err := strconv.ParseFloat(line, 64); err == nil {
			return normalize(f), nil
		}
		// Otherwise scan for the first numeric token on the line.
		for _, field := range strings.Fields(line) {
			if f, err := strconv.ParseFloat(field, 64); err == nil {
				return normalize(f), nil
			}
		}
	}
	return 0, fmt.Errorf("no score found")
}

// normalize maps a 0..100 judge score to [0, 1]; values already in [0, 1] are
// passed through, and out-of-range values are clamped.
func normalize(f float64) float64 {
	switch {
	case f <= 1.0:
		// Treat as already-normalized (e.g. a model returns 0.8).
		if f < 0 {
			return 0
		}
		return f
	case f > 100:
		return 1.0
	default:
		return f / 100.0
	}
}

// buildJudgePrompt renders rubrics and the trace into a scoring instruction.
// The trace is flattened to user/assistant text plus tool calls so the judge
// sees the agent's behavior without raw serialization noise.
func buildJudgePrompt(rubrics []string, trace []message.Message) string {
	var b strings.Builder
	b.WriteString("You are an evaluation judge. Score the agent's response against the rubrics.\n\n")
	b.WriteString("Rubrics:\n")
	for i, r := range rubrics {
		fmt.Fprintf(&b, "%d. %s\n", i+1, r)
	}
	b.WriteString("\nAgent trace (user/assistant turns and tool calls):\n")
	for _, msg := range trace {
		role := string(msg.Role)
		text := strings.TrimSpace(msg.Content().Text)
		switch msg.Role {
		case message.User:
			fmt.Fprintf(&b, "[%s] %s\n", role, text)
		case message.Assistant:
			fmt.Fprintf(&b, "[%s] %s\n", role, text)
			for _, call := range msg.ToolCalls() {
				input := strings.TrimSpace(call.Input)
				if len(input) > 200 {
					input = input[:200] + "..."
				}
				fmt.Fprintf(&b, "  tool %s(%s)\n", call.Name, input)
			}
		}
	}
	b.WriteString("\nReturn exactly one line in the form: SCORE: <number 0..100>\n")
	b.WriteString("where 100 means the response fully satisfies the rubrics and 0 means it fails them.\n")
	return b.String()
}
