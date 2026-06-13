package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/package-register/mocode/internal/workspace"
)

// stubWorkspace records the last call to AgentCancelSubagent so the
// handler test can assert it without standing up a full app.
type stubWorkspace struct {
	workspace.Workspace
	lastSubagentID atomic.Value // string
	calls          atomic.Int32
	shouldFail     bool
}

func (s *stubWorkspace) AgentCancelSubagent(subagentID string) {
	s.calls.Add(1)
	s.lastSubagentID.Store(subagentID)
}

// asServer wraps a stubWorkspace into a Server so we can drive
// handleCancelSubagent directly. The struct literal copies only the
// fields the handler touches; everything else stays zero.
func newTestServerForCancel(w *stubWorkspace) *Server {
	return &Server{workspace: w}
}

// runCancelRequest drives the cancel handler through a real ServeMux so
// that r.PathValue("subagent_id") is populated correctly.
func runCancelRequest(t *testing.T, srv *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc(
		"POST /api/sessions/{session_id}/subagents/{subagent_id}/cancel",
		srv.handleCancelSubagent,
	)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

const (
	testSubagentID = "agent-7"
)

func TestHandleCancelSubagent_DispatchesToWorkspace(t *testing.T) {
	stub := &stubWorkspace{}
	srv := newTestServerForCancel(stub)

	rec := runCancelRequest(
		t,
		srv,
		"/api/sessions/sess-1/subagents/"+testSubagentID+"/cancel",
	)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	if got := stub.calls.Load(); got != 1 {
		t.Fatalf("expected 1 cancel call, got %d", got)
	}
	if got, _ := stub.lastSubagentID.Load().(string); got != testSubagentID {
		t.Fatalf("expected subagent_id=%q, got %q", testSubagentID, got)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if body["ok"] != true {
		t.Fatalf("expected ok=true, got %#v", body["ok"])
	}
	if body["subagent_id"] != testSubagentID {
		t.Fatalf("expected subagent_id=%q, got %#v", testSubagentID, body["subagent_id"])
	}
}

func TestHandleCancelSubagent_RejectsMissingID(t *testing.T) {
	stub := &stubWorkspace{}
	srv := newTestServerForCancel(stub)

	// Drive the handler with an empty subagent_id directly so we can
	// verify the in-handler guard without relying on the router to
	// produce a 404.
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		"/api/sessions/sess-1/subagents//cancel",
		strings.NewReader(""),
	)
	req.SetPathValue("subagent_id", "")
	rec := httptest.NewRecorder()
	srv.handleCancelSubagent(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	if got := stub.calls.Load(); got != 0 {
		t.Fatalf("expected 0 cancel calls on rejection, got %d", got)
	}
}

// Ensure the test infra stays in sync with the workspace.Workspace
// interface signature; this is a compile-time reminder that the
// stubWorkspace must keep implementing every method we drive in tests.
var _ workspace.Workspace = (*stubWorkspace)(nil)
