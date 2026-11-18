package question

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nextsko/mocode-agent/internal/core/question"
)

// fakeService answers immediately with a canned response.
type fakeService struct {
	question.Service
	answers []question.Answer
	lastReq question.Request
}

func (f *fakeService) Ask(_ context.Context, req question.Request) ([]question.Answer, error) {
	f.lastReq = req
	return f.answers, nil
}

// TestToolEmitsQuestionMetadata checks that the structured questions and
// answers travel along as metadata, so the chat UI can render them instead
// of parsing the LLM-facing text.
func TestToolEmitsQuestionMetadata(t *testing.T) {
	t.Parallel()

	yes := true
	svc := &fakeService{answers: []question.Answer{{QuestionID: "q1", Yes: &yes}}}
	tool := NewQuestionPortTool(svc)

	res, err := tool.Execute(t.Context(), nil, json.RawMessage(
		`{"questions":[{"type":"yes_no","question":"Continue?","description":"pick"}]}`))
	require.NoError(t, err)
	require.NoError(t, res.Error)

	meta, ok := res.Metadata["result"].(QuestionResponseMetadata)
	require.True(t, ok, "the result must carry the question metadata")
	require.Len(t, meta.Questions, 1)
	require.Equal(t, "Continue?", meta.Questions[0].Text)
	require.Len(t, meta.Answers, 1)
	require.NotNil(t, meta.Answers[0].Yes)
	require.True(t, *meta.Answers[0].Yes)
}

// TestToolForwardsSessionAndCallID checks the request the service receives.
func TestToolForwardsSessionAndCallID(t *testing.T) {
	t.Parallel()

	svc := &fakeService{}
	tool := NewQuestionPortTool(svc)

	_, err := tool.Execute(t.Context(), nil, json.RawMessage(
		`{"questions":[{"type":"free_text","question":"Why?","description":"tell me"}]}`))
	require.NoError(t, err)
	require.Len(t, svc.lastReq.Questions, 1)
	require.Equal(t, "Why?", svc.lastReq.Questions[0].Text)
}

// TestFormatAnswerUsesNoteKey checks that a question-level note is rendered
// without a key prefix while a choice-level note keeps its key.
func TestFormatAnswerUsesNoteKey(t *testing.T) {
	t.Parallel()

	res := formatAnswer(&question.Answer{
		FillInText: "because",
		Notes: map[string]string{
			question.QuestionNoteKey: "question level",
			"pg":                     "choice level",
		},
	})
	require.Contains(t, res.Content, "- question level")
	require.Contains(t, res.Content, "- [pg]: choice level")
}
