package web

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/package-register/mocode/internal/httputil"
	"github.com/package-register/mocode/internal/workspace"
)

//go:embed dist/*
var webFS embed.FS

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     httputil.AllowLocalOrigin,
}

// Server serves the web UI with REST API + WebSocket.
type Server struct {
	workspace       workspace.Workspace
	mu              sync.Mutex
	server          *http.Server
	listener        net.Listener
	url             string
	planModes       map[string]bool // sessionID -> plan mode state
	archivedState   map[string]bool // sessionID -> archived state
	runningSessions map[string]bool // sessionID -> running state
}

type Status struct {
	Running bool   `json:"running"`
	URL     string `json:"url,omitempty"`
}

func New(ws workspace.Workspace) *Server {
	return &Server{
		workspace:       ws,
		planModes:       make(map[string]bool),
		archivedState:   make(map[string]bool),
		runningSessions: make(map[string]bool),
	}
}

func (s *Server) setPlanMode(sessionID string, enabled bool) {
	s.mu.Lock()
	s.planModes[sessionID] = enabled
	s.mu.Unlock()
}

func (s *Server) getPlanMode(sessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.planModes[sessionID]
}

func (s *Server) setArchived(sessionID string, archived bool) {
	s.mu.Lock()
	s.archivedState[sessionID] = archived
	s.mu.Unlock()
}

func (s *Server) isArchived(sessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.archivedState[sessionID]
}

func (s *Server) setRunning(sessionID string, running bool) {
	s.mu.Lock()
	s.runningSessions[sessionID] = running
	s.mu.Unlock()
}

func (s *Server) isRunning(sessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runningSessions[sessionID]
}

func parseIntParam(r *http.Request, name string, defaultVal int) int {
	val := r.URL.Query().Get(name)
	if val == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(val)
	if err != nil || n < 0 {
		return defaultVal
	}
	return n
}

func (s *Server) Start(ctx context.Context, host string, port int) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server != nil {
		return s.url, nil
	}

	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		// Try random port if specified one is busy
		ln, err = net.Listen("tcp", net.JoinHostPort(host, "0"))
		if err != nil {
			return "", fmt.Errorf("web: listen failed: %w", err)
		}
	}

	mux := http.NewServeMux()
	s.routes(mux)

	srv := &http.Server{
		Handler:           httputil.RequestLogging("web", mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	s.server = srv
	s.listener = ln
	s.url = "http://" + ln.Addr().String()

	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Warn("web server stopped", "error", err)
		}
	}()

	go func() {
		<-ctx.Done()
		_ = s.Stop(context.Background())
	}()

	return s.url, nil
}

func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopLocked(ctx)
}

func (s *Server) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Status{Running: s.server != nil, URL: s.url}
}

func (s *Server) stopLocked(ctx context.Context) error {
	if s.server == nil {
		return nil
	}
	err := s.server.Shutdown(ctx)
	s.server = nil
	s.listener = nil
	s.url = ""
	return err
}

func (s *Server) routes(mux *http.ServeMux) {
	// Health check
	mux.HandleFunc("GET /healthz", s.handleHealth)

	// Config
	mux.HandleFunc("GET /api/config/", s.handleGetConfig)
	mux.HandleFunc("PATCH /api/config/", s.handlePatchConfig)
	mux.HandleFunc("GET /api/config/toml", s.handleGetConfigToml)
	mux.HandleFunc("PUT /api/config/toml", s.handlePutConfigToml)

	// Sessions
	mux.HandleFunc("GET /api/sessions/", s.handleListSessions)
	mux.HandleFunc("POST /api/sessions/", s.handleCreateSession)
	mux.HandleFunc("GET /api/sessions/{session_id}", s.handleGetSession)
	mux.HandleFunc("DELETE /api/sessions/{session_id}", s.handleDeleteSession)
	mux.HandleFunc("PATCH /api/sessions/{session_id}", s.handlePatchSession)
	mux.HandleFunc("POST /api/sessions/{session_id}/files", s.handleUploadFile)
	mux.HandleFunc("GET /api/sessions/{session_id}/uploads/{path...}", s.handleGetUpload)
	mux.HandleFunc("GET /api/sessions/{session_id}/files/{path...}", s.handleGetWorkspaceFile)
	mux.HandleFunc("POST /api/sessions/{session_id}/generate-title", s.handleGenerateTitle)
	mux.HandleFunc("POST /api/sessions/{session_id}/fork", s.handleForkSession)
	mux.HandleFunc("GET /api/sessions/{session_id}/git-diff", s.handleGitDiff)
	// Stop a single sub-agent dispatched by the Agent tool.
	mux.HandleFunc("POST /api/sessions/{session_id}/subagents/{subagent_id}/cancel", s.handleCancelSubagent)

	// Work dirs
	mux.HandleFunc("GET /api/work-dirs/", s.handleListWorkDirs)
	mux.HandleFunc("GET /api/work-dirs/startup", s.handleGetStartupDir)

	// Open in
	mux.HandleFunc("POST /api/open-in", s.handleOpenIn)

	// WebSocket session stream
	mux.HandleFunc("GET /api/sessions/{session_id}/stream", s.handleWebSocketStream)

	// Static file serving (SPA fallback — must be last)
	if sub, err := fs.Sub(webFS, "dist"); err == nil {
		fileServer := http.FileServer(http.FS(sub))
		mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
			// If the request is for a static file that exists, serve it.
			// Otherwise serve index.html for SPA client-side routing.
			path := r.URL.Path
			if path == "/" {
				fileServer.ServeHTTP(w, r)
				return
			}
			// Try to open the file; if it doesn't exist, serve index.html
			f, err := sub.Open(strings.TrimPrefix(path, "/"))
			if err != nil {
				r.URL.Path = "/"
				fileServer.ServeHTTP(w, r)
				return
			}
			f.Close()
			fileServer.ServeHTTP(w, r)
		})
	}
}

// -- Handlers that can be satisfied inline --

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"status": "ok", "url": s.url})
}

func loggedHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		slog.Debug("web", "method", r.Method, "path", r.URL.Path)
		next.ServeHTTP(w, r)
		slog.Debug("web", "method", r.Method, "path", r.URL.Path, "dur", time.Since(start))
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"detail": msg})
}

func writeFileResponse(w http.ResponseWriter, content []byte, mediaType string) {
	w.Header().Set("Content-Type", mediaType)
	_, _ = w.Write(content)
}

func getStartupDir() string {
	dir, _ := os.Getwd()
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}
	return dir
}
