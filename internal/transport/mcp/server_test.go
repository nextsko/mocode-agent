package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// fakeRunner records calls and returns scripted output.
type fakeRunner struct {
	prompts []string
	dirs    []string
	models  string
	err     error
}

func (f *fakeRunner) RunPrompt(_ context.Context, prompt, cwd string, out io.Writer) error {
	f.prompts = append(f.prompts, prompt)
	f.dirs = append(f.dirs, cwd)
	if f.err != nil {
		return f.err
	}
	_, _ = io.WriteString(out, "answer:"+prompt)
	return nil
}

func (f *fakeRunner) ListModels(_ context.Context, out io.Writer) error {
	if f.err != nil {
		return f.err
	}
	_, _ = io.WriteString(out, f.models)
	return nil
}

func (f *fakeRunner) Version() string { return "9.9.9" }

// serve runs the server over the given request lines and returns the decoded
// responses.
func serve(t *testing.T, runner Runner, lines ...string) []map[string]any {
	t.Helper()

	var out strings.Builder
	srv := New(runner)
	err := srv.Serve(context.Background(), strings.NewReader(strings.Join(lines, "\n")+"\n"), &out)
	require.NoError(t, err)

	var responses []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var resp map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &resp), "response must be JSON: %s", line)
		responses = append(responses, resp)
	}
	return responses
}

func TestServeHandshake(t *testing.T) {
	t.Parallel()

	responses := serve(t, &fakeRunner{},
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"ping"}`,
	)

	require.Len(t, responses, 2, "notifications must not be answered")

	init := responses[0]
	require.Equal(t, "2.0", init["jsonrpc"])
	require.EqualValues(t, 1, init["id"])
	result, ok := init["result"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "2025-06-18", result["protocolVersion"])
	serverInfo := result["serverInfo"].(map[string]any)
	require.Equal(t, "mocode", serverInfo["name"])
	require.Equal(t, "9.9.9", serverInfo["version"])
}

func TestServeFallsBackForUnknownProtocol(t *testing.T) {
	t.Parallel()

	responses := serve(t, &fakeRunner{},
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`,
	)
	result := responses[0]["result"].(map[string]any)
	require.Equal(t, fallbackProtocolVersion, result["protocolVersion"])
}

func TestToolsList(t *testing.T) {
	t.Parallel()

	responses := serve(t, &fakeRunner{}, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	result := responses[0]["result"].(map[string]any)
	tools, ok := result["tools"].([]any)
	require.True(t, ok)
	require.Len(t, tools, 2)

	names := make([]string, 0, len(tools))
	for _, raw := range tools {
		names = append(names, raw.(map[string]any)["name"].(string))
	}
	require.ElementsMatch(t, []string{"run", "models"}, names)
}

func TestToolsCallRun(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{}
	responses := serve(t, runner,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"run","arguments":{"prompt":"hi","cwd":"/tmp"}}}`,
	)

	require.Equal(t, []string{"hi"}, runner.prompts)
	require.Equal(t, []string{"/tmp"}, runner.dirs)

	result := responses[0]["result"].(map[string]any)
	require.NotContains(t, result, "isError")
	content := result["content"].([]any)
	require.Len(t, content, 1)
	require.Equal(t, "answer:hi", content[0].(map[string]any)["text"])
}

func TestToolsCallRunError(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{err: errors.New("boom")}
	responses := serve(t, runner,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"run","arguments":{"prompt":"hi"}}}`,
	)
	result := responses[0]["result"].(map[string]any)
	require.Equal(t, true, result["isError"])
	content := result["content"].([]any)
	require.Contains(t, content[0].(map[string]any)["text"], "boom")
}

func TestToolsCallModels(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{models: "openai/gpt-5\n"}
	responses := serve(t, runner,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"models","arguments":{}}}`,
	)
	result := responses[0]["result"].(map[string]any)
	content := result["content"].([]any)
	require.Equal(t, "openai/gpt-5", content[0].(map[string]any)["text"])
}

func TestUnknownMethodAndTool(t *testing.T) {
	t.Parallel()

	responses := serve(t, &fakeRunner{},
		`{"jsonrpc":"2.0","id":1,"method":"no/such/method"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"nope","arguments":{}}}`,
	)

	require.Len(t, responses, 2)
	errObj := responses[0]["error"].(map[string]any)
	require.EqualValues(t, -32601, errObj["code"])

	result := responses[1]["result"].(map[string]any)
	require.Equal(t, true, result["isError"])
}

func TestMalformedLineIsIgnored(t *testing.T) {
	t.Parallel()

	responses := serve(t, &fakeRunner{},
		`not json`,
		`{"jsonrpc":"2.0","id":1,"method":"ping"}`,
	)
	require.Len(t, responses, 1)
}
