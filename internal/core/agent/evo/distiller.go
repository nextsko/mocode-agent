package evo

import (
	"context"
	"fmt"
	"strings"

	"charm.land/fantasy"
)

// LLMDistiller returns a LessonDistiller backed by a language model. For each
// successful turn it asks the model to extract a single generalized, reusable
// principle (not the literal task), which is what makes the optimal-theory
// reconstruction genuinely emergent: successive turns contribute abstracted
// lessons rather than verbatim prompts. Distillation mirrors the hermes GEPA
// insight — read the turn, propose a targeted, transferable improvement.
//
// On any error the distiller falls back to the trimmed prompt (the default),
// so a model failure never blocks the evo loop. It is synchronous; callers in
// hot paths may run it in a goroutine since the seam returns a plain string.
func LLMDistiller(ctx context.Context, model fantasy.LanguageModel) LessonDistiller {
	return func(prompt, response string) string {
		principle, err := distill(ctx, model, prompt, response)
		if err != nil || strings.TrimSpace(principle) == "" {
			return strings.TrimSpace(prompt)
		}
		return strings.TrimSpace(principle)
	}
}

// distill asks the model for one transferable principle demonstrated by the
// turn. The prompt is deliberately terse and forbids restating the task, so
// the output is a generalized rule the agent can apply to future turns.
func distill(ctx context.Context, model fantasy.LanguageModel, prompt, response string) (string, error) {
	if model == nil {
		return "", fmt.Errorf("evo distiller: model is required")
	}
	task := strings.TrimSpace(prompt)
	if task == "" {
		return "", nil
	}
	out, err := model.Generate(ctx, fantasy.Call{
		Prompt: fantasy.Prompt{
			fantasy.NewSystemMessage(
				"You distill agent experience into transferable principles. " +
					"Given a successful task and the agent's response, extract ONE " +
					"concise, generalizable principle (an instruction the agent should " +
					"always follow), in a single line, imperative voice. " +
					"Do not restate the task. Do not add preamble."),
			fantasy.NewUserMessage(fmt.Sprintf(
				"Successful task:\n%s\n\nAgent response excerpt:\n%s\n\n"+
					"One transferable principle:",
				task, truncate(response, 1200))),
		},
	})
	if err != nil {
		return "", err
	}
	return out.Content.Text(), nil
}

// truncate caps a string to n runes for the distillation prompt.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}
