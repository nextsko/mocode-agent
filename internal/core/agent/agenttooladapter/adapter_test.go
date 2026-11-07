package agenttooladapter

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"charm.land/fantasy"
	"github.com/stretchr/testify/require"

	"github.com/nextsko/mocode-agent/internal/core/tools"
)

func fantasyCall(input string) fantasy.ToolCall { return fantasy.ToolCall{Input: input} }

// fakeTool is a scriptable tools.Tool.
type fakeTool struct {
	schema  tools.Schema
	result  tools.ToolResult
	err     error
	gotArgs json.RawMessage
	gotCtx  tools.ToolContext
	ran     bool
}

func (f *fakeTool) Name() string        { return f.schema.Name }
func (f *fakeTool) Description() string { return f.schema.Description }
func (f *fakeTool) Schema() tools.Schema {
	return f.schema
}

func (f *fakeTool) Execute(_ context.Context, tctx tools.ToolContext, args json.RawMessage) (tools.ToolResult, error) {
	f.ran = true
	f.gotCtx = tctx
	f.gotArgs = args
	return f.result, f.err
}

func TestInfoCarriesSchema(t *testing.T) {
	t.Parallel()

	params := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string"},
		},
		"required": []any{"query"},
	}
	tool := &fakeTool{schema: tools.Schema{
		Name:        "demo",
		Description: "demo tool",
		Parameters:  params,
	}}
	adapter := New(tool, nil)

	info := adapter.Info()
	require.Equal(t, "demo", info.Name)
	require.Equal(t, "demo tool", info.Description)
	require.Equal(t, params, info.Parameters)
	require.Equal(t, []string{"query"}, info.Required)
}

func TestRunPassesArgumentsAndMapsText(t *testing.T) {
	t.Parallel()

	tool := &fakeTool{
		schema: tools.Schema{Name: "demo"},
		result: tools.ToolResult{
			Content:  "hello",
			Metadata: map[string]any{"k": "v"},
		},
	}
	adapter := New(tool, nil)
	resp, err := adapter.Run(t.Context(), fantasyCall(`{"x":1}`))
	require.NoError(t, err)
	require.Equal(t, "hello", resp.Content)
	require.False(t, resp.IsError)
	require.JSONEq(t, `{"k":"v"}`, resp.Metadata)
	require.JSONEq(t, `{"x":1}`, string(tool.gotArgs))
	require.NotNil(t, tool.gotCtx, "adapter must always pass a non-nil context")
}

func TestRunMapsToolResultError(t *testing.T) {
	t.Parallel()

	tool := &fakeTool{
		schema: tools.Schema{Name: "demo"},
		result: tools.ToolResult{Error: errors.New("boom")},
	}
	resp, err := New(tool, nil).Run(t.Context(), fantasyCall(`{}`))
	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "boom")
}

func TestRunPropagatesExecuteError(t *testing.T) {
	t.Parallel()

	tool := &fakeTool{schema: tools.Schema{Name: "demo"}, err: errors.New("hard failure")}
	_, err := New(tool, nil).Run(t.Context(), fantasyCall(`{}`))
	require.Error(t, err)
}

func TestRunMapsImageAttachment(t *testing.T) {
	t.Parallel()

	tool := &fakeTool{
		schema: tools.Schema{Name: "demo"},
		result: tools.ToolResult{
			Content: "shot",
			Attachments: []tools.Attachment{{
				MimeType: "image/png",
				Content:  []byte{0x89, 0x50},
			}},
		},
	}
	resp, err := New(tool, nil).Run(t.Context(), fantasyCall(`{}`))
	require.NoError(t, err)
	require.Equal(t, []byte{0x89, 0x50}, resp.Data)
	require.Equal(t, "image/png", resp.MediaType)
}

type fakeOptsData struct{}

func (fakeOptsData) MarshalJSON() ([]byte, error) { return []byte(`"v"`), nil }
func (fakeOptsData) UnmarshalJSON([]byte) error   { return nil }
func (fakeOptsData) Options()                     {}

func TestNewWithContextBuildsFreshContextPerCall(t *testing.T) {
	t.Parallel()

	calls := 0
	cleanups := 0
	adapter := NewWithContext(&fakeTool{schema: tools.Schema{Name: "demo"}}, func(context.Context) (tools.ToolContext, func()) {
		calls++
		return noContext{}, func() { cleanups++ }
	})

	for range 2 {
		_, err := adapter.Run(t.Context(), fantasyCall(`{}`))
		require.NoError(t, err)
	}
	require.Equal(t, 2, calls, "factory must run once per Execute")
	require.Equal(t, 2, cleanups, "cleanup must run once per Execute")
}

func TestProviderOptionsRoundTrip(t *testing.T) {
	t.Parallel()

	adapter := New(&fakeTool{schema: tools.Schema{Name: "demo"}}, nil)
	opts := fantasy.ProviderOptions{"k": fakeOptsData{}}
	adapter.SetProviderOptions(opts)
	require.Equal(t, opts, adapter.ProviderOptions())
}
