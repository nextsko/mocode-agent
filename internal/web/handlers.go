package web

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/package-register/mocode/internal/config"
	"github.com/package-register/mocode/internal/httputil"
	"github.com/package-register/mocode/internal/session"
	"github.com/package-register/mocode/internal/session/message"
	"github.com/package-register/mocode/internal/store"
	"github.com/package-register/mocode/internal/workspace"
)

// --- Response types (aligned with web frontend API) ---

type WebSession struct {
	SessionID   string     `json:"session_id"`
	Title       string     `json:"title"`
	LastUpdated time.Time  `json:"last_updated"`
	IsRunning   bool       `json:"is_running"`
	Status      *WebStatus `json:"status,omitempty"`
	WorkDir     *string    `json:"work_dir,omitempty"`
	SessionDir  *string    `json:"session_dir,omitempty"`
	Archived    bool       `json:"archived"`
}

type WebStatus struct {
	SessionID string    `json:"session_id"`
	State     string    `json:"state"`
	Seq       int       `json:"seq"`
	UpdatedAt time.Time `json:"updated_at"`
}

type GlobalConfigResponse struct {
	DefaultModel    string            `json:"default_model"`
	DefaultThinking bool              `json:"default_thinking"`
	Yolo            bool              `json:"yolo"`
	Models          []ConfigModelInfo `json:"models"`
}

type ConfigModelInfo struct {
	Provider       string   `json:"provider"`
	Model          string   `json:"model"`
	MaxContextSize int64    `json:"max_context_size"`
	Name           string   `json:"name"`
	ProviderType   string   `json:"provider_type"`
	Capabilities   []string `json:"capabilities,omitempty"`
}

type UploadFileResponse struct {
	Path     string `json:"path"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
}

type GenerateTitleResponse struct {
	Title string `json:"title"`
}

type CreateSessionRequest struct {
	WorkDir   *string `json:"work_dir,omitempty"`
	CreateDir bool    `json:"create_dir,omitempty"`
}

type UpdateSessionRequest struct {
	Title    *string `json:"title,omitempty"`
	Archived *bool   `json:"archived,omitempty"`
}

type ForkSessionRequest struct {
	TurnIndex int `json:"turn_index"`
}

type OpenInRequest struct {
	App  string `json:"app"`
	Path string `json:"path"`
}

type OpenInResponse struct {
	Ok     bool    `json:"ok"`
	Detail *string `json:"detail,omitempty"`
}

type GitDiffResponse struct {
	IsGitRepo      bool   `json:"is_git_repo"`
	HasChanges     bool   `json:"has_changes"`
	TotalAdditions int    `json:"total_additions"`
	TotalDeletions int    `json:"total_deletions"`
	Files          []any  `json:"files"`
	Error          string `json:"error,omitempty"`
}

// --- Helpers ---

func (s *Server) webSessionFromSession(sess session.Session, workDir string) WebSession {
	ws := WebSession{
		SessionID:   sess.ID,
		Title:       sess.Title,
		LastUpdated: time.Unix(sess.UpdatedAt, 0),
		Archived:    s.isArchived(sess.ID),
		IsRunning:   s.isRunning(sess.ID),
	}
	if workDir != "" {
		ws.WorkDir = &workDir
		sessDir := filepath.Join(workDir, ".mocode", "sessions", sess.ID)
		ws.SessionDir = &sessDir
	}
	return ws
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// --- Config handlers ---

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	cfg := s.workspace.Config()

	resp := GlobalConfigResponse{
		DefaultModel:    "",
		DefaultThinking: false,
		Yolo:            false,
		Models:          []ConfigModelInfo{},
	}

	if cfg == nil {
		writeJSON(w, resp)
		return
	}

	// Get default model from config
	if m, ok := cfg.Models[config.SelectedModelTypeLarge]; ok {
		resp.DefaultModel = m.Model
	}
	// Read thinking mode from the selected model (CanReason flag)
	if model := cfg.GetModelByType(config.SelectedModelTypeLarge); model != nil {
		resp.DefaultThinking = model.CanReason
	}
	resp.Yolo = s.workspace.PermissionSkipRequests()

	// Build model list from providers
	for provEntry := range cfg.Providers.Seq() {
		for _, model := range provEntry.Models {
			info := ConfigModelInfo{
				Name:           model.ID,
				Model:          model.ID,
				Provider:       provEntry.ID,
				ProviderType:   string(provEntry.Type),
				MaxContextSize: model.ContextWindow,
			}
			if model.SupportsImages {
				info.Capabilities = append(info.Capabilities, "image_in")
			}
			if model.CanReason {
				info.Capabilities = append(info.Capabilities, "thinking")
			}
			resp.Models = append(resp.Models, info)
		}
	}

	writeJSON(w, resp)
}

func (s *Server) handlePatchConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DefaultModel             *string `json:"default_model,omitempty"`
		DefaultThinking          *bool   `json:"default_thinking,omitempty"`
		Yolo                     *bool   `json:"yolo,omitempty"`
		RestartRunningSessions   *bool   `json:"restart_running_sessions,omitempty"`
		ForceRestartBusySessions *bool   `json:"force_restart_busy_sessions,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if req.DefaultModel != nil {
		// Find which provider has this model
		cfg := s.workspace.Config()
		if cfg != nil {
			for prov := range cfg.Providers.Seq() {
				for _, m := range prov.Models {
					if m.ID == *req.DefaultModel {
						_ = s.workspace.UpdatePreferredModel(
							config.ScopeGlobal,
							config.SelectedModelTypeLarge,
							config.SelectedModel{
								Model:    m.ID,
								Provider: prov.ID,
							},
						)
						goto done
					}
				}
			}
		}
	}
done:
	if req.Yolo != nil {
		if !httputil.IsLoopbackRequest(r) {
			writeError(w, http.StatusForbidden, "yolo mode can only be changed from localhost")
			return
		}
		s.workspace.PermissionSetSkipRequests(*req.Yolo)
	}

	// Return response in the shape frontend expects.
	cfg := s.workspace.Config()
	resp := GlobalConfigResponse{
		DefaultModel:    "",
		DefaultThinking: false,
		Yolo:            false,
		Models:          []ConfigModelInfo{},
	}
	if cfg != nil {
		if m, ok := cfg.Models[config.SelectedModelTypeLarge]; ok {
			resp.DefaultModel = m.Model
		}
		resp.Yolo = s.workspace.PermissionSkipRequests()
		for provEntry := range cfg.Providers.Seq() {
			for _, model := range provEntry.Models {
				resp.Models = append(resp.Models, ConfigModelInfo{
					Name:           model.ID,
					Model:          model.ID,
					Provider:       provEntry.ID,
					ProviderType:   string(provEntry.Type),
					MaxContextSize: model.ContextWindow,
				})
			}
		}
	}
	writeJSON(w, map[string]any{
		"config":                   resp,
		"restarted_session_ids":    []string{},
		"skipped_busy_session_ids": []string{},
	})
}

func (s *Server) handleGetConfigToml(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{"content": "", "path": ""}

	// Try multiple config paths in order of priority
	paths := []string{}
	if p := config.GlobalConfigData(); p != "" {
		paths = append(paths, p)
	}
	if p := config.GlobalConfig(); p != "" {
		paths = append(paths, p)
	}
	// Also check working dir for local config
	if wd := s.workspace.WorkingDir(); wd != "" {
		paths = append(paths,
			filepath.Join(wd, "mocode.json"),
			filepath.Join(wd, ".mocode.json"),
		)
	}

	for _, p := range paths {
		if data, err := os.ReadFile(p); err == nil {
			resp["path"] = p
			resp["content"] = string(data)
			break
		}
	}
	writeJSON(w, resp)
}

func (s *Server) handlePutConfigToml(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if p := config.GlobalConfigData(); p != "" {
		if err := store.WriteJSON(p, req.Content); err != nil {
			writeJSON(w, map[string]any{"success": false, "error": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"success": true})
		return
	}
	writeJSON(w, map[string]any{"success": false, "error": "no config file"})
}

// --- Session handlers ---

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	// Parse query params
	q := strings.ToLower(r.URL.Query().Get("q"))
	limit := parseIntParam(r, "limit", 100)
	offset := parseIntParam(r, "offset", 0)
	archivedStr := r.URL.Query().Get("archived")
	filterArchived := archivedStr == "true"

	sessions, err := s.workspace.ListSessions(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	result := make([]WebSession, 0, len(sessions))
	for _, sess := range sessions {
		// Search filter
		if q != "" && !strings.Contains(strings.ToLower(sess.Title), q) &&
			!strings.Contains(strings.ToLower(sess.ID), q) {
			continue
		}
		ws := s.webSessionFromSession(sess, s.workspace.WorkingDir())
		ws.Archived = s.isArchived(sess.ID)
		// Archived filter: if archived=true, show only archived; otherwise only non-archived
		if archivedStr != "" && ws.Archived != filterArchived {
			continue
		}
		result = append(result, ws)
	}

	// Apply pagination
	if offset >= len(result) {
		writeJSON(w, []WebSession{})
		return
	}
	end := offset + limit
	if end > len(result) {
		end = len(result)
	}

	writeJSON(w, result[offset:end])
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req CreateSessionRequest
	if r.Body != nil && r.ContentLength > 0 {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	title := "Session " + time.Now().Format("2006-01-02 15:04")
	sess, err := s.workspace.CreateSession(r.Context(), title)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Use custom work_dir if provided
	workDir := s.workspace.WorkingDir()
	if req.WorkDir != nil && *req.WorkDir != "" {
		workDir = *req.WorkDir
		if req.CreateDir {
			os.MkdirAll(workDir, 0o755)
		}
	}

	writeJSON(w, s.webSessionFromSession(sess, workDir))
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("session_id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "missing session_id")
		return
	}

	sess, err := s.workspace.GetSession(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	writeJSON(w, s.webSessionFromSession(sess, s.workspace.WorkingDir()))
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("session_id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "missing session_id")
		return
	}

	if err := s.workspace.DeleteSession(r.Context(), sessionID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handlePatchSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("session_id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "missing session_id")
		return
	}

	var req UpdateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	sess, err := s.workspace.GetSession(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	if req.Title != nil {
		sess.Title = *req.Title
	}
	if req.Archived != nil {
		s.setArchived(sessionID, *req.Archived)
	}
	if _, err := s.workspace.SaveSession(r.Context(), sess); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, s.webSessionFromSession(sess, s.workspace.WorkingDir()))
}

// --- File upload ---

func (s *Server) handleUploadFile(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("session_id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "missing session_id")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 100<<20)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "failed to parse form: "+err.Error())
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing file field")
		return
	}
	defer file.Close()

	// Find session directory
	workDir := s.workspace.WorkingDir()
	uploadsDir := filepath.Join(workDir, ".mocode", "uploads", sessionID)
	if err := os.MkdirAll(uploadsDir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	filename := filepath.Base(header.Filename)
	if filename == "" || filename == "." {
		filename = "unnamed"
	}

	dst, err := os.Create(filepath.Join(uploadsDir, filename))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer dst.Close()

	written, err := dst.ReadFrom(file)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, UploadFileResponse{
		Path:     filepath.Join("uploads", sessionID, filename),
		Filename: filename,
		Size:     written,
	})
}

func (s *Server) handleGetUpload(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("session_id")
	filePath := r.PathValue("path")

	workDir := s.workspace.WorkingDir()
	fullPath := filepath.Join(workDir, ".mocode", "uploads", sessionID, filePath)
	fullPath = filepath.Clean(fullPath)
	uploadsDir := filepath.Clean(filepath.Join(workDir, ".mocode", "uploads", sessionID))
	if !strings.HasPrefix(fullPath, uploadsDir) {
		writeError(w, http.StatusForbidden, "path traversal")
		return
	}

	data, err := os.ReadFile(fullPath)
	if err != nil {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}

	writeFileResponse(w, data, detectContentType(fullPath))
}

func (s *Server) handleGetWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("session_id")
	filePath := r.PathValue("path")

	workDir := s.workspace.WorkingDir()

	// Normalize path: treat "." and "" as empty (directory listing)
	if filePath == "." || filePath == "" {
		sessDir := filepath.Join(workDir, ".mocode", "sessions", sessionID)
		entries, err := os.ReadDir(sessDir)
		if err != nil {
			writeJSON(w, []any{})
			return
		}
		var listing []map[string]any
		for _, entry := range entries {
			info := map[string]any{"name": entry.Name(), "type": "file"}
			if entry.IsDir() {
				info["type"] = "directory"
			}
			listing = append(listing, info)
		}
		writeJSON(w, listing)
		return
	}

	fullPath := filepath.Join(workDir, filePath)
	// Path traversal protection
	cleanWorkDir := filepath.Clean(workDir)
	cleanFullPath := filepath.Clean(fullPath)
	if !strings.HasPrefix(cleanFullPath, cleanWorkDir+string(os.PathSeparator)) && cleanFullPath != cleanWorkDir {
		writeError(w, http.StatusForbidden, "path traversal denied")
		return
	}

	data, err := os.ReadFile(fullPath)
	if err != nil {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}

	writeFileResponse(w, data, detectContentType(fullPath))
}

// --- Generate title ---

func (s *Server) handleGenerateTitle(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("session_id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "missing session_id")
		return
	}

	// Try heuristic first using workspace directory.
	workDir := s.workspace.WorkingDir()
	title := extractTitleFromFile(sessionID, workDir)
	if title == "" {
		// Fallback: use first user message text.
		msgs, err := s.workspace.ListUserMessages(r.Context(), sessionID)
		if err == nil && len(msgs) > 0 {
			text := msgs[0].Content().Text
			if len(text) > 50 {
				text = text[:50] + "..."
			}
			title = text
		}
	}
	if title == "" {
		title = "Untitled"
	}

	writeJSON(w, GenerateTitleResponse{Title: title})
}

// --- Fork session (with message copy) ---

func (s *Server) handleForkSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("session_id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "missing session_id")
		return
	}

	var req ForkSessionRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	sess, err := s.workspace.GetSession(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	// Create a new session
	newSess, err := s.workspace.CreateSession(r.Context(), "Fork: "+sess.Title)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Copy wire.jsonl from source session to new session
	workDir := s.workspace.WorkingDir()
	srcWire := filepath.Join(workDir, ".mocode", "sessions", sessionID, "wire.jsonl")
	dstDir := filepath.Join(workDir, ".mocode", "sessions", newSess.ID)
	if err := os.MkdirAll(dstDir, 0o755); err == nil {
		dstWire := filepath.Join(dstDir, "wire.jsonl")
		if src, err := os.Open(srcWire); err == nil {
			defer src.Close()
			if dst, err := os.Create(dstWire); err == nil {
				defer dst.Close()
				io.Copy(dst, src)
			}
		}
	}

	// Copy messages up to turn_index if specified via message service.
	if appWS, ok := s.workspace.(*workspace.AppWorkspace); ok {
		appInstance := appWS.App()
		msgs, listErr := s.workspace.ListMessages(r.Context(), sessionID)
		if listErr == nil {
			turnCount := 0
			for _, m := range msgs {
				if m.Role == message.User {
					turnCount++
				}
				if req.TurnIndex > 0 && turnCount > req.TurnIndex {
					break
				}
				// Create a copy of the message in the new session
				_, _ = appInstance.Messages.Create(r.Context(), newSess.ID, message.CreateMessageParams{
					Role:     m.Role,
					Parts:    m.Parts,
					Model:    m.Model,
					Provider: m.Provider,
					Sender:   m.Sender,
				})
			}
		}
	}

	writeJSON(w, s.webSessionFromSession(newSess, s.workspace.WorkingDir()))
}

// --- Git diff (simplified) ---

func (s *Server) handleGitDiff(w http.ResponseWriter, r *http.Request) {
	workDir := s.workspace.WorkingDir()
	gitDir := filepath.Join(workDir, ".git")

	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		writeJSON(w, GitDiffResponse{IsGitRepo: false})
		return
	}

	// Get file statuses from git diff --name-status
	statusMap := map[string]string{}
	statusCmd := exec.Command("git", "-C", workDir, "diff", "--name-status")
	if statusOut, err := statusCmd.Output(); err == nil {
		statusMap = parseGitNameStatus(string(statusOut))
	}

	// Run git diff --stat to get real diff stats.
	cmd := exec.Command("git", "-C", workDir, "diff", "--stat")
	out, err := cmd.Output()
	if err != nil {
		writeJSON(w, GitDiffResponse{IsGitRepo: true, HasChanges: false, Files: []any{}})
		return
	}

	stats := parseGitDiffStat(string(out), statusMap)
	writeJSON(w, GitDiffResponse{
		IsGitRepo:      true,
		HasChanges:     stats.TotalAdditions > 0 || stats.TotalDeletions > 0,
		TotalAdditions: stats.TotalAdditions,
		TotalDeletions: stats.TotalDeletions,
		Files:          stats.Files,
	})
}

type gitDiffStat struct {
	TotalAdditions int
	TotalDeletions int
	Files          []any
}

func parseGitNameStatus(output string) map[string]string {
	statusMap := map[string]string{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Format: "M\tfile.go" or "R100\told.js\tnew.js"
		parts := strings.Split(line, "\t")
		if len(parts) < 2 {
			continue
		}
		statusCode := strings.TrimSpace(parts[0])
		// For renamed files, the new path is the last tab-separated part.
		filename := strings.TrimSpace(parts[len(parts)-1])

		switch {
		case len(statusCode) == 0:
			// Skip empty status lines (shouldn't happen after TrimSpace but defend)
			continue
		case statusCode[0] == 'A':
			statusMap[filename] = "added"
		case statusCode[0] == 'D':
			statusMap[filename] = "deleted"
		case statusCode[0] == 'R':
			statusMap[filename] = "renamed"
		case statusCode[0] == 'M':
			statusMap[filename] = "modified"
		default:
			statusMap[filename] = "modified"
		}
	}
	return statusMap
}

func parseGitDiffStat(output string, statusMap map[string]string) gitDiffStat {
	var result gitDiffStat
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, "|") == false {
			continue
		}
		// Parse lines like: " file.go | 10 +++++-----"
		parts := strings.Split(line, "|")
		if len(parts) != 2 {
			continue
		}
		filename := strings.TrimSpace(parts[0])
		changePart := strings.TrimSpace(parts[1])
		// Count + and - signs
		additions := strings.Count(changePart, "+")
		deletions := strings.Count(changePart, "-")
		result.TotalAdditions += additions
		result.TotalDeletions += deletions

		status := statusMap[filename]
		if status == "" {
			status = "modified" // default fallback
		}

		result.Files = append(result.Files, map[string]any{
			"path":      filename,
			"additions": additions,
			"deletions": deletions,
			"status":    status,
		})
	}
	return result
}

// --- Work dirs ---

func (s *Server) handleListWorkDirs(w http.ResponseWriter, r *http.Request) {
	dir := s.workspace.WorkingDir()
	if dir != "" {
		writeJSON(w, []string{dir})
	} else {
		writeJSON(w, []string{})
	}
}

func (s *Server) handleGetStartupDir(w http.ResponseWriter, r *http.Request) {
	dir := s.workspace.WorkingDir()
	if dir == "" {
		dir, _ = os.Getwd()
	}
	writeJSON(w, dir)
}

// handleCancelSubagent stops a single sub-agent dispatched by the Agent
// tool. The sub-agent's subagentID is the user-visible identifier reported
// on SubagentCompleted events (e.g. "<parentToolCallID>-1"); the parent
// session is left running.
func (s *Server) handleCancelSubagent(w http.ResponseWriter, r *http.Request) {
	subagentID := r.PathValue("subagent_id")
	if subagentID == "" {
		writeError(w, http.StatusBadRequest, "missing subagent_id")
		return
	}
	s.workspace.AgentCancelSubagent(subagentID)
	writeJSON(w, map[string]any{"ok": true, "subagent_id": subagentID})
}

// --- Open in ---

func (s *Server) handleOpenIn(w http.ResponseWriter, r *http.Request) {
	var req OpenInRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	err := openInApp(req.App, req.Path)
	if err != nil {
		writeJSON(w, OpenInResponse{Ok: false, Detail: strPtr(err.Error())})
		return
	}
	writeJSON(w, OpenInResponse{Ok: true})
}

func openInApp(app, path string) error {
	switch app {
	case "vscode", "cursor":
		return openEditor(path)
	case "finder":
		return openFinder(path)
	case "terminal":
		return openTerminal(path)
	case "iterm":
		return openITerm(path)
	case "antigravity":
		return openAntigravity(path)
	}
	return os.ErrNotExist
}

func openEditor(path string) error {
	switch runtime.GOOS {
	case "windows":
		// Try code.cmd first (VS Code), then fallback to cursor
		if err := runCmd("code.cmd", path); err == nil {
			return nil
		}
		if err := runCmd("code", path); err == nil {
			return nil
		}
		return runCmd("cursor", path)
	default:
		if _, err := os.Stat("/usr/bin/code"); err == nil {
			return runCmd("code", path)
		}
		if _, err := os.Stat("/usr/local/bin/code"); err == nil {
			return runCmd("/usr/local/bin/code", path)
		}
		// Try cursor if code not found
		if _, err := os.Stat("/usr/bin/cursor"); err == nil {
			return runCmd("cursor", path)
		}
		return os.ErrNotExist
	}
}

func openFinder(path string) error {
	switch runtime.GOOS {
	case "windows":
		return runCmd("explorer", "/select,"+path)
	case "darwin":
		return runCmd("open", "-R", path)
	default:
		if _, err := os.Stat("/usr/bin/nautilus"); err == nil {
			return runCmd("nautilus", path)
		}
		if _, err := os.Stat("/usr/bin/xdg-open"); err == nil {
			return runCmd("xdg-open", filepath.Dir(path))
		}
		return os.ErrNotExist
	}
}

func openTerminal(path string) error {
	switch runtime.GOOS {
	case "windows":
		// Try Windows Terminal first, fallback to cmd
		if err := runCmd("wt", "-d", path); err == nil {
			return nil
		}
		return runCmd("cmd", "/c", "start", "cmd", "/k", "cd", "/d", path)
	case "darwin":
		return runCmd("open", "-a", "Terminal", path)
	default:
		return runCmd("x-terminal-emulator", "--working-directory", path)
	}
}

func openITerm(path string) error {
	switch runtime.GOOS {
	case "darwin":
		return runCmd("open", "-a", "iTerm", path)
	default:
		// Fallback to default terminal on non-macOS
		return openTerminal(path)
	}
}

func openAntigravity(path string) error {
	switch runtime.GOOS {
	case "darwin":
		return runCmd("open", "-a", "Antigravity", path)
	default:
		return os.ErrNotExist // Antigravity is macOS-only
	}
}

func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	return cmd.Run()
}

// --- Helpers ---

func detectContentType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js":
		return "application/javascript; charset=utf-8"
	case ".json":
		return "application/json"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".svg":
		return "image/svg+xml"
	case ".woff":
		return "font/woff"
	case ".woff2":
		return "font/woff2"
	case ".ttf":
		return "font/ttf"
	case ".md":
		return "text/markdown; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}

func extractTitleFromFile(sessionID, workDir string) string {
	wireFile := filepath.Join(workDir, ".mocode", "sessions", sessionID, "wire.jsonl")
	data, err := os.ReadFile(wireFile)
	if err != nil {
		return ""
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var record struct {
			Message *struct {
				Type    string `json:"type"`
				Payload *struct {
					UserInput any `json:"user_input"`
				} `json:"payload"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			continue
		}
		if record.Message == nil || record.Message.Type != "TurnBegin" {
			continue
		}
		if record.Message.Payload == nil {
			continue
		}
		text, ok := record.Message.Payload.UserInput.(string)
		if !ok || text == "" {
			continue
		}
		if len(text) > 50 {
			text = text[:50] + "..."
		}
		return text
	}
	return ""
}
