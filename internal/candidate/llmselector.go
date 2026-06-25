package candidate

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"charm.land/fantasy"

	"github.com/package-register/mocode/internal/session/message"
)

// LLMSelector picks the best candidate by asking a judge model to rank the
// candidate responses. It is the default online selector for best-of-N when no
// evaluation criterion is available: the judge sees the original prompt and all
// candidate responses, then returns the 1-based index of the best one.
//
// The judge model is a fully-constructed fantasy.LanguageModel (typically the
// configured small model), injected by the caller.
type LLMSelector struct {
	// Model is the judge. Required.
	Model fantasy.LanguageModel
	// Prompt is the original user prompt the candidates are responding to.
	// Required so the judge can evaluate relevance to the question.
	Prompt string
	// MaxOutputTokens caps the judge response. Defaults to 256.
	MaxOutputTokens int64
}

// Compile-time assertion that LLMSelector satisfies Selector.
var _ Selector = (*LLMSelector)(nil)

// Select asks the judge to pick the best viable candidate. Failed candidates
// (Err != nil) are excluded from the ranking. The judge returns a 1-based index
// among the presented candidates; Select maps it back to the original slice.
func (s *LLMSelector) Select(ctx context.Context, candidates []Result) (int, error) {
	if s.Model == nil {
		return -1, fmt.Errorf("llmselector: judge model is required")
	}
	// Collect viable candidates and build a numbered presentation.
	var list []viableCandidate
	for i, c := range candidates {
		if c.Err != nil {
			continue
		}
		list = append(list, viableCandidate{origIdx: i, text: finalText(c.Trace)})
	}
	if len(list) == 0 {
		return -1, fmt.Errorf("llmselector: no viable candidate to judge")
	}
	if len(list) == 1 {
		return list[0].origIdx, nil
	}

	maxTokens := s.MaxOutputTokens
	if maxTokens == 0 {
		maxTokens = 256
	}

	agent := fantasy.NewAgent(s.Model)
	res, err := agent.Generate(ctx, fantasy.AgentCall{
		Prompt:          buildJudgePrompt(s.Prompt, list),
		MaxOutputTokens: &maxTokens,
	})
	if err != nil {
		return -1, fmt.Errorf("llmselector: judge generate: %w", err)
	}
	text := strings.TrimSpace(res.Response.Content.Text())

	pick, err := parsePick(text, len(list))
	if err != nil {
		// If the judge is unparseable, fall back to the first viable candidate
		// rather than failing the whole turn. Best-of-N degrades gracefully.
		return list[0].origIdx, nil
	}
	return list[pick-1].origIdx, nil
}

// viableCandidate pairs a candidate's original slice index with its response text.
type viableCandidate struct {
	origIdx int
	text    string
}

// buildJudgePrompt presents the prompt and numbered candidates.
func buildJudgePrompt(prompt string, list []viableCandidate) string {
	var b strings.Builder
	b.WriteString("You are judging candidate responses to a user prompt. ")
	b.WriteString("Pick the single best response by quality, correctness, and helpfulness.\n\n")
	b.WriteString("User prompt:\n")
	b.WriteString(prompt)
	b.WriteString("\n\nCandidate responses:\n")
	for i, v := range list {
		fmt.Fprintf(&b, "--- Candidate %d ---\n%s\n\n", i+1, v.text)
	}
	b.WriteString("Return exactly one line with the number of the best candidate (e.g. \"2\").")
	return b.String()
}

// parsePick extracts a 1-based index from the judge response.
func parsePick(text string, count int) (int, error) {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Try the whole line as a number.
		if n, err := strconv.Atoi(line); err == nil && n >= 1 && n <= count {
			return n, nil
		}
		// Scan tokens for a number.
		for _, field := range strings.Fields(line) {
			if n, err := strconv.Atoi(field); err == nil && n >= 1 && n <= count {
				return n, nil
			}
		}
	}
	return 0, fmt.Errorf("llmselector: could not parse pick from %q", text)
}

// finalText extracts the last assistant text from a trace, matching the
// convention used by criterion.FinalText. Duplicated here to keep the candidate
// package free of an evaluation dependency in this selector path.
func finalText(trace []message.Message) string {
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
