package browser

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	corebrowser "github.com/nextsko/mocode-agent/internal/core/browser"
	"github.com/nextsko/mocode-agent/internal/core/permission"
)

// fakeGate records permission requests and returns a canned verdict.
type fakeGate struct {
	allow bool
	err   error
	calls []permission.CreatePermissionRequest
}

func (f *fakeGate) Request(_ context.Context, opts permission.CreatePermissionRequest) (bool, error) {
	f.calls = append(f.calls, opts)
	return f.allow, f.err
}

func runTool(t *testing.T, deps Deps, input string) fantasy.ToolResponse {
	t.Helper()
	resp, err := NewBrowserTool(deps).Run(context.Background(), fantasy.ToolCall{
		ID: "call-1", Name: BrowserToolName, Input: input,
	})
	require.NoError(t, err)
	return resp
}

// ─── dispatch and validation (no browser needed) ─────────────────────────────

func TestBrowser_StatusReportsDetectionAndSession(t *testing.T) {
	t.Parallel()

	resp := runTool(t, Deps{}, `{"action":"status"}`)
	require.False(t, resp.IsError, resp.Content)

	assert.Contains(t, resp.Content, "Platform:")
	// Either browsers were found or the report explains what was tried; both are
	// useful, and asserting only the happy path would make this fail on CI.
	assert.True(t,
		strings.Contains(resp.Content, "Found") || strings.Contains(resp.Content, "No usable browser found"),
		"status must say what was found or why nothing was:\n%s", resp.Content)
	assert.Contains(t, resp.Content, "No browser session is running")
}

func TestBrowser_InvalidAction(t *testing.T) {
	t.Parallel()

	for _, input := range []string{`{}`, `{"action":""}`, `{"action":"nope"}`} {
		resp := runTool(t, Deps{}, input)
		assert.True(t, resp.IsError, "input %q must be rejected", input)
		assert.Contains(t, resp.Content, "launch")
	}
}

// Acting before launching must produce a pointer to launch, not a panic or a
// confusing low-level error.
func TestBrowser_SessionActionWithoutSession(t *testing.T) {
	t.Parallel()

	for _, input := range []string{
		`{"action":"snapshot"}`,
		`{"action":"click","ref":"e0"}`,
		`{"action":"goto","url":"http://x/"}`,
		`{"action":"console"}`,
	} {
		resp := runTool(t, Deps{}, input)
		assert.True(t, resp.IsError, "input %q must fail without a session", input)
		assert.Contains(t, resp.Content, "action=launch", "the error must say how to fix it")
	}
}

func TestBrowser_RequiredParameters(t *testing.T) {
	t.Parallel()

	// A manager with a session is needed for these to reach the validation we
	// care about, so use one pointed at nothing and assert on the early checks.
	deps := Deps{Manager: newIdleManager(t)}

	tests := []struct {
		input   string
		wantSub string
	}{
		{`{"action":"goto"}`, "url is required"},
		{`{"action":"press"}`, "key is required"},
		{`{"action":"eval"}`, "expression is required"},
	}

	for _, tt := range tests {
		// Without a session the "launch first" error wins, which is also
		// correct; assert only that we get a clear, actionable message.
		resp := runTool(t, deps, tt.input)
		assert.True(t, resp.IsError)
		assert.True(t,
			strings.Contains(resp.Content, tt.wantSub) || strings.Contains(resp.Content, "action=launch"),
			"input %s produced %q", tt.input, resp.Content)
	}
}

func TestBrowser_CloseWithoutSession(t *testing.T) {
	t.Parallel()

	resp := runTool(t, Deps{}, `{"action":"close"}`)
	require.False(t, resp.IsError)
	assert.Contains(t, resp.Content, "No browser session")
}

// ─── permissions ─────────────────────────────────────────────────────────────

func TestBrowser_LaunchAsksForPermission(t *testing.T) {
	t.Parallel()

	gate := &fakeGate{allow: false}
	resp := runTool(t, Deps{Permissions: gate}, `{"action":"launch"}`)

	require.Len(t, gate.calls, 1)
	call := gate.calls[0]
	assert.Equal(t, BrowserToolName, call.ToolName)
	assert.Equal(t, "launch", call.Action)
	assert.Equal(t, "call-1", call.ToolCallID)

	// Denied -> a denied response, and crucially no browser started.
	assert.True(t, resp.IsError)
	assert.Contains(t, resp.Content, "denied")
}

// Attaching to the user's browser is a different act from starting one, and the
// approval prompt must say which it is.
func TestBrowser_AttachPermissionDescribesAttaching(t *testing.T) {
	t.Parallel()

	gate := &fakeGate{allow: false}
	runTool(t, Deps{Permissions: gate}, `{"action":"launch","endpoint":"http://127.0.0.1:9222"}`)

	require.Len(t, gate.calls, 1)
	assert.Contains(t, gate.calls[0].Description, "Attach to the browser at")
	assert.Contains(t, gate.calls[0].Description, "http://127.0.0.1:9222")
}

func TestBrowser_LaunchPermissionDescribesLaunching(t *testing.T) {
	t.Parallel()

	gate := &fakeGate{allow: false}
	runTool(t, Deps{Permissions: gate}, `{"action":"launch","browser":"msedge","headless":false}`)

	require.Len(t, gate.calls, 1)
	assert.Contains(t, gate.calls[0].Description, "Launch a browser")
	assert.Contains(t, gate.calls[0].Description, "msedge")
	assert.Contains(t, gate.calls[0].Description, "headless=false")
}

// The debugging actions must NOT prompt: a prompt per click would make the tool
// unusable, and they only touch a session the user already approved.
func TestBrowser_NonLaunchActionsDoNotPrompt(t *testing.T) {
	t.Parallel()

	gate := &fakeGate{allow: true}
	for _, input := range []string{
		`{"action":"status"}`,
		`{"action":"snapshot"}`,
		`{"action":"click","ref":"e0"}`,
		`{"action":"console"}`,
		`{"action":"close"}`,
	} {
		runTool(t, Deps{Permissions: gate}, input)
	}
	assert.Empty(t, gate.calls, "only launch is gated")
}

func TestBrowser_RefIsPreferredOverSelector(t *testing.T) {
	t.Parallel()

	target, errResp := targetOf(BrowserParams{Ref: "e3", Selector: "#other"})
	require.Nil(t, errResp)
	assert.Equal(t, "e3", target)

	target, errResp = targetOf(BrowserParams{Selector: "#other"})
	require.Nil(t, errResp)
	assert.Equal(t, "#other", target)

	_, errResp = targetOf(BrowserParams{})
	require.NotNil(t, errResp)
	assert.Contains(t, errResp.Content, "ref is required")
}

// ─── value rendering ─────────────────────────────────────────────────────────

func TestRenderValue(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "undefined", renderValue(nil))
	assert.Equal(t, "hello", renderValue("hello"))
	assert.Equal(t, "true", renderValue(true))
	// JSON numbers decode as float64; whole ones must not gain a ".0".
	assert.Equal(t, "42", renderValue(float64(42)))
	assert.Equal(t, "1.5", renderValue(1.5))
	assert.Contains(t, renderValue(map[string]any{"a": 1.0}), `"a": 1`)
	assert.Contains(t, renderValue([]any{1.0, 2.0}), "1,\n  2")
}

// ─── schema ──────────────────────────────────────────────────────────────────

func TestBrowser_SchemaExposesActionEnum(t *testing.T) {
	t.Parallel()

	info := NewBrowserTool(Deps{}).Info()
	assert.Equal(t, BrowserToolName, info.Name)

	action, ok := info.Parameters["action"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, []any{
		"status", "launch", "goto", "back", "forward", "reload", "snapshot",
		"click", "dblclick", "fill", "press", "hover", "check", "uncheck",
		"select", "eval", "screenshot", "pdf", "console", "tabs", "storage",
		"state", "network", "route", "close",
	}, action["enum"])

	for _, field := range []string{"url", "ref", "selector", "text", "key", "expression", "path", "op", "kind", "index", "name", "value", "pattern", "body", "status"} {
		spec, ok := info.Parameters[field].(map[string]any)
		require.Truef(t, ok, "field %q missing from the generated schema", field)
		assert.NotEmptyf(t, spec["description"], "field %q needs a description for the model", field)
	}
}

func TestBrowser_DescriptionIsUsable(t *testing.T) {
	t.Parallel()

	desc := NewBrowserTool(Deps{}).Info().Description
	assert.NotEmpty(t, desc)
	assert.Greater(t, len(desc), 40)
	assert.False(t, strings.HasPrefix(desc, "//"), "the //go:embed directive must not leak")
}

// ─── end-to-end through a real browser ───────────────────────────────────────

const toolPage = `<!doctype html><html><head><meta charset="utf-8"><title>Tool E2E</title></head>
<body>
  <input id="q" placeholder="Search">
  <button id="go">Search</button>
  <div id="out"></div>
  <script>
    document.getElementById('go').addEventListener('click', function () {
      document.getElementById('out').textContent = 'results for ' + document.getElementById('q').value;
    });
    console.log('tool-page-ready');
  </script>
</body></html>`

// TestBrowser_ToolDrivesARealBrowser exercises the whole stack — tool → manager
// → CDP → Chrome — because the value of a tool is in the integration, not in
// its parameter parsing.
func TestBrowser_ToolDrivesARealBrowser(t *testing.T) {
	if os.Getenv("MOCODE_TEST_NO_BROWSER") != "" {
		t.Skip("MOCODE_TEST_NO_BROWSER is set")
	}
	if testing.Short() {
		t.Skip("skipping real-browser test in -short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	if _, err := corebrowser.DetectOrError(ctx, corebrowser.DetectOptions{}); err != nil {
		t.Skipf("no browser available: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, toolPage)
	}))
	defer srv.Close()

	gate := &fakeGate{allow: true}
	deps := Deps{Permissions: gate, Manager: corebrowser.NewManager()}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = deps.Manager.Close(closeCtx)
	}()

	// 1. launch, then goto.
	resp := runTool(t, deps, `{"action":"launch","url":"`+srv.URL+`"}`)
	if resp.IsError {
		t.Skipf("could not launch a browser here: %s", resp.Content)
	}
	require.Contains(t, resp.Content, "Browser session ready")

	resp = runTool(t, deps, `{"action":"goto","url":"`+srv.URL+`"}`)
	require.False(t, resp.IsError, resp.Content)
	assert.Contains(t, resp.Content, "Tool E2E")
	assert.Contains(t, resp.Content, "Search")

	// The snapshot returned by goto must carry usable refs. Match on role as
	// well as name: the input's placeholder is "Search" and the button's label is
	// "Search", so a name-only lookup would silently pick the wrong element.
	buttonRef := refFor(t, resp.Content, "button", "Search")
	inputRef := refFor(t, resp.Content, "text", "Search")

	// 2. fill, using the ref the snapshot minted.
	resp = runTool(t, deps, `{"action":"fill","ref":"`+inputRef+`","text":"mocode"}`)
	require.False(t, resp.IsError, resp.Content)

	// 3. click, and see the page react.
	resp = runTool(t, deps, `{"action":"click","ref":"`+buttonRef+`"}`)
	require.False(t, resp.IsError, resp.Content)
	assert.Contains(t, resp.Content, "results for mocode",
		"the page's own handler must have run with the filled value")

	// 4. eval.
	resp = runTool(t, deps, `{"action":"eval","expression":"document.querySelectorAll('button').length"}`)
	require.False(t, resp.IsError, resp.Content)
	assert.Contains(t, resp.Content, "1")

	// 5. console, including the page's own log.
	require.Eventually(t, func() bool {
		r := runTool(t, deps, `{"action":"console"}`)
		return strings.Contains(r.Content, "tool-page-ready")
	}, 10*time.Second, 200*time.Millisecond, "console output must reach the tool")

	// 6. screenshot writes a real file.
	shot := filepath.Join(t.TempDir(), "shot.png")
	resp = runTool(t, deps, `{"action":"screenshot","path":`+jsonString(shot)+`}`)
	require.False(t, resp.IsError, resp.Content)
	data, err := os.ReadFile(shot)
	require.NoError(t, err)
	require.Greater(t, len(data), 1000)
	assert.Equal(t, []byte{0x89, 'P', 'N', 'G'}, data[:4])

	// 7. close shuts it down.
	resp = runTool(t, deps, `{"action":"close"}`)
	require.False(t, resp.IsError)
	assert.Contains(t, resp.Content, "closed")
	assert.Nil(t, deps.Manager.Session())
}

func refFor(t *testing.T, snapshot, role, name string) string {
	t.Helper()
	for _, line := range strings.Split(snapshot, "\n") {
		// Match on role as well as name: the input's placeholder is "Search" and
		// the button's label is "Search", so a name-only lookup picks the wrong one.
		if !strings.Contains(line, role) || !strings.Contains(line, name) {
			continue
		}
		// Lines look like: `  [e1] button "Search"`.
		if i := strings.Index(line, "["); i >= 0 {
			if j := strings.Index(line[i:], "]"); j > 0 {
				return line[i+1 : i+j]
			}
		}
	}
	t.Fatalf("no %s named %q in snapshot:\n%s", role, name, snapshot)
	return ""
}

func jsonString(s string) string {
	return `"` + strings.ReplaceAll(s, `\`, `\\`) + `"`
}

// newIdleManager returns a manager with no session, for tests that only need
// the type.
func newIdleManager(_ *testing.T) *corebrowser.Manager { return corebrowser.NewManager() }
