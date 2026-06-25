package roundtable

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContextBuilder_BuildTurnPrompt(t *testing.T) {
	t.Parallel()
	rt, _ := New(Config{
		Topic: "refactor auth",
		Participants: []Participant{
			{Name: "mod", IsModerator: true},
			{Name: "reviewer"},
		},
	})
	rt.AddStatement(Statement{Speaker: "mod", Kind: StatementChat, Content: "let's plan the refactor"})

	builder := NewContextBuilder()
	prompt, err := builder.BuildTurnPrompt(rt, "reviewer")
	require.NoError(t, err)
	require.Contains(t, prompt.SystemPrompt, "reviewer")
	require.Contains(t, prompt.Context, "refactor auth")
	require.Contains(t, prompt.Context, "let's plan the refactor")
	require.Contains(t, prompt.Instructions, "formal motion")
}

func TestContextBuilder_BuildTurnPrompt_UnknownSpeaker(t *testing.T) {
	t.Parallel()
	rt, _ := New(Config{Topic: "x", Participants: []Participant{{Name: "mod", IsModerator: true}}})
	_, err := NewContextBuilder().BuildTurnPrompt(rt, "nobody")
	require.Error(t, err)
}
