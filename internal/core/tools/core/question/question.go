package question

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/core/question"
	"github.com/nextsko/mocode-agent/internal/core/tools/contract"
)

const QuestionToolName = "question"

//go:embed question.md
var questionDescription string

// QuestionParams defines the parameters for the question tool.
type QuestionParams struct {
	Questions          []QuestionItem `json:"questions" description:"List of questions to present. Single item = no tabs, multiple = tabbed form."`
	ConfirmTitle       string         `json:"confirm_title,omitempty" description:"Title for the confirmation tab. Required for multi-question batches."`
	ConfirmDescription string         `json:"confirm_description,omitempty" description:"Description for the confirmation tab. Required for multi-question batches."`
}

// UnmarshalJSON handles models that double-serialize the questions field as a
// JSON string instead of a native array.
func (p *QuestionParams) UnmarshalJSON(data []byte) error {
	type Alias QuestionParams
	aux := &struct {
		Questions json.RawMessage `json:"questions"`
		*Alias
	}{
		Alias: (*Alias)(p),
	}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	if len(aux.Questions) == 0 {
		return nil
	}
	// Try array first.
	if err := json.Unmarshal(aux.Questions, &p.Questions); err != nil {
		// Fall back to string-encoded JSON array.
		var s string
		if err2 := json.Unmarshal(aux.Questions, &s); err2 != nil {
			return err
		}
		if err2 := json.Unmarshal([]byte(strings.TrimSpace(s)), &p.Questions); err2 != nil {
			return fmt.Errorf("questions must be an array: %w", err2)
		}
	}
	return nil
}

// QuestionItem is a single question from the tool input.
type QuestionItem struct {
	Label       string           `json:"label,omitempty" description:"Short tab header label (3 words max)."`
	Type        string           `json:"type" description:"The type of question: yes_no, single_choice, multi_choice, or free_text"`
	Question    string           `json:"question" description:"The question text"`
	Description string           `json:"description" description:"Required markdown description shown below the question"`
	Choices     []QuestionChoice `json:"choices,omitempty" description:"List of choices"`
	Options     []QuestionChoice `json:"options,omitempty"` // alias for Choices
}

// GetChoices returns choices, preferring the Choices field over Options.
func (q QuestionItem) GetChoices() []QuestionChoice {
	if len(q.Choices) > 0 {
		return q.Choices
	}
	return q.Options
}

// QuestionChoice represents a selectable option.
type QuestionChoice struct {
	ID          string `json:"id" description:"Unique identifier for this choice"`
	Label       string `json:"label" description:"Display text for this choice"`
	Description string `json:"description,omitempty" description:"Optional description for this choice"`
}

// NewQuestionPortTool is the framework-agnostic form of the question tool.
func NewQuestionPortTool(svc question.Service) contract.Tool {
	return contract.NewFunc(
		QuestionToolName,
		questionDescription,
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"questions": map[string]any{
					"type":        "array",
					"description": "List of questions to present. Single item = no tabs, multiple = tabbed form.",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"label":       map[string]any{"type": "string", "description": "Short tab header label (3 words max)."},
							"type":        map[string]any{"type": "string", "description": "The type of question: yes_no, single_choice, multi_choice, or free_text"},
							"question":    map[string]any{"type": "string", "description": "The question text"},
							"description": map[string]any{"type": "string", "description": "Required markdown description shown below the question"},
							"choices":     map[string]any{"type": "array", "items": map[string]any{"type": "object"}, "description": "List of choices"},
							"options":     map[string]any{"type": "array", "items": map[string]any{"type": "object"}, "description": "Alias for choices"},
						},
					},
				},
				"confirm_title":       map[string]any{"type": "string", "description": "Title for the confirmation tab."},
				"confirm_description": map[string]any{"type": "string", "description": "Description for the confirmation tab."},
			},
			"required": []string{"questions"},
		},
		func(ctx context.Context, _ contract.ToolContext, args json.RawMessage) (contract.ToolResult, error) {
			var params QuestionParams
			if err := json.Unmarshal(args, &params); err != nil {
				return contract.ToolResult{}, fmt.Errorf("invalid parameters: %w", err)
			}
			callID, _ := toolutil.ToolCallIDFromCtx(ctx)
			return runQuestion(ctx, svc, params, callID)
		})
}

// runQuestion validates the batch and asks the user.
func runQuestion(ctx context.Context, svc question.Service, params QuestionParams, callID string) (contract.ToolResult, error) {
	sessionID := toolutil.GetSessionFromContext(ctx)

	if len(params.Questions) == 0 {
		return contract.Error("at least one question is required"), nil
	}
	if len(params.Questions) > question.MaxQuestions {
		return contract.Error(fmt.Sprintf("exceeds maximum of %d questions per batch (got %d). Split into multiple batches and tell the user there will be follow-up questions", question.MaxQuestions, len(params.Questions))), nil
	}

	questions := make([]question.Question, len(params.Questions))
	for i, item := range params.Questions {
		qType := question.Type(item.Type)
		if qType != question.TypeYesNo && qType != question.TypeSingleChoice && qType != question.TypeMultiChoice && qType != question.TypeFreeText {
			label := item.Label
			if label == "" {
				label = item.Question
			}
			return contract.Error(fmt.Sprintf("question %d [%s]: invalid type %q (must be yes_no, single_choice, multi_choice, or free_text)", i+1, label, item.Type)), nil
		}
		questions[i] = question.Question{
			Type:        qType,
			Label:       item.Label,
			Text:        item.Question,
			Description: item.Description,
			Choices:     convertChoices(item.GetChoices()),
		}
	}

	req := question.Request{
		SessionID:          sessionID,
		ToolCallID:         callID,
		Questions:          questions,
		ConfirmTitle:       params.ConfirmTitle,
		ConfirmDescription: params.ConfirmDescription,
	}

	answers, err := svc.Ask(ctx, req)
	if err != nil {
		if errors.Is(err, question.ErrCancelled) {
			res := contract.Error("User cancelled this question")
			res.StopTurn = true
			return res, nil
		}
		return contract.Error(err.Error()), nil
	}

	return formatAnswers(answers, questions)
}

func convertChoices(in []QuestionChoice) []question.Choice {
	out := make([]question.Choice, len(in))
	for i, c := range in {
		out[i] = question.Choice{ID: c.ID, Label: c.Label, Description: c.Description}
	}
	return out
}

// formatAnswers converts answers into a tool response string for the LLM.
func formatAnswers(answers []question.Answer, questions []question.Question) (contract.ToolResult, error) {
	var b strings.Builder
	for i, answer := range answers {
		if i > 0 {
			b.WriteString("\n\n")
		}
		if i < len(questions) {
			fmt.Fprintf(&b, "Q%d: %s\n", i+1, questions[i].Text)
		}
		formatted := formatAnswer(&answer)
		b.WriteString(formatted.Content)
	}
	return contract.Text(b.String()), nil
}

// formatAnswer formats a single answer for the LLM.
func formatAnswer(answer *question.Answer) contract.ToolResult {
	var b strings.Builder

	switch {
	case answer.Yes != nil:
		if *answer.Yes {
			b.WriteString("User answered: yes")
		} else {
			b.WriteString("User answered: no")
		}
	case len(answer.SelectedIDs) > 0 || answer.FillInText != "":
		// Selections and free-text can coexist for multi-choice;
		// report both so the model sees the complete answer.
		var parts []string
		if len(answer.SelectedIDs) > 0 {
			data, _ := json.Marshal(answer.SelectedIDs)
			parts = append(parts, fmt.Sprintf("User selected: %s", string(data)))
		}
		if answer.FillInText != "" {
			parts = append(parts, fmt.Sprintf("User provided: %s", answer.FillInText))
		}
		b.WriteString(strings.Join(parts, "\n"))
	default:
		b.WriteString("User skipped this question")
	}

	if len(answer.Notes) > 0 {
		b.WriteString("\n\nNotes:")
		for key, note := range answer.Notes {
			if key == "_question" {
				fmt.Fprintf(&b, "\n- %s", note)
			} else {
				fmt.Fprintf(&b, "\n- [%s]: %s", key, note)
			}
		}
	}

	return contract.Text(b.String())
}
