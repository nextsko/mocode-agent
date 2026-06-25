package evo

import (
	"context"
	"errors"
	"testing"

	"charm.land/fantasy"

	"github.com/stretchr/testify/require"
)

// fakeModel is a minimal LanguageModel that returns a canned principle.
type fakeModel struct {
	text string
	err  error
}

func (f fakeModel) Provider() string { return "fake" }
func (f fakeModel) Model() string    { return "fake-model" }
func (f fakeModel) Stream(context.Context, fantasy.Call) (fantasy.StreamResponse, error) {
	return nil, errors.New("not implemented")
}

func (f fakeModel) GenerateObject(context.Context, fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, errors.New("not implemented")
}

func (f fakeModel) StreamObject(context.Context, fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, errors.New("not implemented")
}

func (f fakeModel) Generate(_ context.Context, _ fantasy.Call) (*fantasy.Response, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &fantasy.Response{Content: fantasy.ResponseContent{
		fantasy.TextContent{Text: f.text},
	}}, nil
}

func TestLLMDistiller_ExtractsPrinciple(t *testing.T) {
	t.Parallel()
	d := LLMDistiller(context.Background(), fakeModel{text: "Always verify file paths before editing"})
	got := d("edit the auth handler", "edited successfully")
	require.Equal(t, "Always verify file paths before editing", got)
}

func TestLLMDistiller_FallsBackOnError(t *testing.T) {
	t.Parallel()
	d := LLMDistiller(context.Background(), fakeModel{err: errors.New("boom")})
	got := d("edit the auth handler", "edited successfully")
	// On model error the distiller falls back to the trimmed prompt.
	require.Equal(t, "edit the auth handler", got)
}

func TestLLMDistiller_NilModelFallsBack(t *testing.T) {
	t.Parallel()
	d := LLMDistiller(context.Background(), nil)
	require.Equal(t, "do the thing", d("do the thing", "done"))
}
