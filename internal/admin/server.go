package admin

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
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/package-register/mocode/internal/agent/tools/mcp"
	"github.com/package-register/mocode/internal/config"
	"github.com/package-register/mocode/internal/knowledge/memory"
	"github.com/package-register/mocode/internal/minimax"
	wechat "github.com/package-register/mocode/internal/wechat"
	"github.com/package-register/mocode/internal/workspace"
)

//go:embed assets/*
var assetsFS embed.FS

type Server struct {
	workspace workspace.Workspace
	mu        sync.Mutex
	server    *http.Server
	listener  net.Listener
	url       string
	wechat    wechatState
}

type Status struct {
	Running bool   `json:"running"`
	URL     string `json:"url,omitempty"`
}

type ConfigFieldRequest struct {
	Scope config.Scope `json:"scope"`
	Key   string       `json:"key"`
	Value any          `json:"value"`
}

type ProxyRequest struct {
	Enabled  bool   `json:"enabled"`
	ProxyURL string `json:"proxy_url"`
	NoProxy  string `json:"no_proxy"`
}

type MiniMaxProviderRequest struct {
	ProviderID       string   `json:"provider_id"`
	Name             string   `json:"name"`
	Type             string   `json:"type"`
	BaseURL          string   `json:"base_url"`
	APIKey           string   `json:"api_key"`
	QuotaBaseURL     string   `json:"quota_base_url"`
	QuotaCookie      string   `json:"quota_cookie"`
	ActiveModel      string   `json:"active_model"`
	DefaultMaxTokens int64    `json:"default_max_tokens"`
	ReasoningEffort  string   `json:"reasoning_effort"`
	Models           []string `json:"models"`
}

type MCPServerRequest struct {
	Name          string            `json:"name"`
	Type          config.MCPType    `json:"type"`
	Command       string            `json:"command"`
	Args          []string          `json:"args"`
	Env           map[string]string `json:"env"`
	URL           string            `json:"url"`
	Headers       map[string]string `json:"headers"`
	Timeout       int               `json:"timeout"`
	Disabled      bool              `json:"disabled"`
	DisabledTools []string          `json:"disabled_tools"`
}

type MCPServerNameRequest struct {
	Name string `json:"name"`
}

type ProviderSaveRequest struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	BaseURL    string   `json:"base_url"`
	APIKey     string   `json:"api_key"`
	Models     []string `json:"models"`
	MaxTokens  int64    `json:"default_max_tokens"`
	Reasoning  string   `json:"reasoning_effort"`
	SetAsLarge bool     `json:"set_as_large"`
	SetAsSmall bool     `json:"set_as_small"`
}

type ProviderDeleteRequest struct {
	ID string `json:"id"`
}

type ImportRequest struct {
	Mode    string `json:"mode"`    // "merge" | "replace"
	Content string `json:"content"` // JSON string
}

type ProviderSummary struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	BaseURL    string `json:"base_url"`
	HasAPIKey  bool   `json:"has_api_key"`
	ModelCount int    `json:"model_count"`
}

type MCPToggleRequest struct {
	Name    string `json:"name"`
	Enable  bool   `json:"enable"`
	Persist bool   `json:"persist"`
}

type ToolInfo struct {
	Server      string `json:"server"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	InputSchema any    `json:"input_schema,omitempty"`
}

type wechatState struct {
	mu      sync.Mutex
	status  string
	qr      string
	qrImage string
	userID  string
	err     string
	active  bool
}

func New(workspace workspace.Workspace) *Server {
	return &Server{workspace: workspace}
}

func (s *Server) Start(ctx context.Context, port int) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server != nil {
		return s.url, nil
	}
	addr := "127.0.0.1:" + strconv.Itoa(port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", err
	}
	mux := http.NewServeMux()
	s.routes(mux)
	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	s.server = srv
	s.listener = ln
	s.url = "http://" + ln.Addr().String()
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Warn("admin server stopped", "error", err)
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
	mux.HandleFunc("GET /", s.handleIndex)
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(assetSubFS()))))
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("GET /api/config", s.handleConfig)
	mux.HandleFunc("POST /api/config/set", s.handleSetConfig)
	mux.HandleFunc("GET /api/config/export", s.handleConfigExport)
	mux.HandleFunc("POST /api/config/import", s.handleConfigImport)
	mux.HandleFunc("POST /api/proxy", s.handleProxy)
	mux.HandleFunc("GET /api/providers", s.handleProviders)
	mux.HandleFunc("GET /api/providers/catalog", s.handleProvidersCatalog)
	mux.HandleFunc("POST /api/providers/save", s.handleProviderSave)
	mux.HandleFunc("POST /api/providers/delete", s.handleProviderDelete)
	mux.HandleFunc("GET /api/minimax/defaults", s.handleMiniMaxDefaults)
	mux.HandleFunc("POST /api/minimax/provider", s.handleMiniMaxProvider)
	mux.HandleFunc("POST /api/minimax/quota", s.handleMiniMaxQuota)
	mux.HandleFunc("GET /api/mcp/servers", s.handleMCPServers)
	mux.HandleFunc("POST /api/mcp/server", s.handleMCPServer)
	mux.HandleFunc("POST /api/mcp/server/delete", s.handleMCPServerDelete)
	mux.HandleFunc("POST /api/mcp/server/toggle", s.handleMCPServerToggle)
	mux.HandleFunc("GET /api/mcp/tools", s.handleMCPTools)
	mux.HandleFunc("POST /api/wechat/start", s.handleWeChatStart)
	mux.HandleFunc("GET /api/wechat/status", s.handleWeChatStatus)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(indexHTML))
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.Status())
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.workspace.Config())
}

// handleProviders lists all configured providers.
func (s *Server) handleProviders(w http.ResponseWriter, r *http.Request) {
	cfg := s.workspace.Config()
	providers := make([]ProviderSummary, 0)
	for id, item := range cfg.Providers.Seq2() {
		providers = append(providers, ProviderSummary{
			ID:         id,
			Name:       cmpTrim(item.Name, id),
			Type:       string(item.Type),
			BaseURL:    item.BaseURL,
			HasAPIKey:  strings.TrimSpace(item.APIKey) != "",
			ModelCount: len(item.Models),
		})
	}
	// Sort by ID for stable output.
	sort.Slice(providers, func(i, j int) bool {
		return providers[i].ID < providers[j].ID
	})
	writeJSON(w, providers)
}

// handleProvidersCatalog lists all available providers from the catwalk catalog.
func (s *Server) handleProvidersCatalog(w http.ResponseWriter, r *http.Request) {
	catwalkProviders, err := config.Providers(s.workspace.Config())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	type CatalogEntry struct {
		ID          string          `json:"id"`
		Name        string          `json:"name"`
		Type        string          `json:"type"`
		APIEndpoint string          `json:"api_endpoint"`
		Models      []catwalk.Model `json:"models"`
		ModelCount  int             `json:"model_count"`
	}
	result := make([]CatalogEntry, 0, len(catwalkProviders))
	for _, p := range catwalkProviders {
		id := string(p.ID)
		result = append(result, CatalogEntry{
			ID:          id,
			Name:        cmpTrim(p.Name, id),
			Type:        string(p.Type),
			APIEndpoint: p.APIEndpoint,
			Models:      p.Models,
			ModelCount:  len(p.Models),
		})
	}
	writeJSON(w, result)
}

// handleProviderSave creates or updates a provider.
func (s *Server) handleProviderSave(w http.ResponseWriter, r *http.Request) {
	var req ProviderSaveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	providerID := strings.TrimSpace(req.ID)
	if providerID == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("provider id is required"))
		return
	}
	if !validConfigName(providerID) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid provider id"))
		return
	}

	baseURL := strings.TrimSpace(req.BaseURL)
	apiKey := strings.TrimSpace(req.APIKey)

	// Preserve existing API key if not provided.
	if apiKey == "" {
		if existing, ok := s.workspace.Config().Providers.Get(providerID); ok {
			apiKey = existing.APIKey
		}
	}

	// Build models from IDs.
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 65536
	}
	reasoning := cmpTrim(req.Reasoning, "medium")
	models := make([]catwalk.Model, 0)
	for _, mid := range req.Models {
		mid = strings.TrimSpace(mid)
		if mid == "" {
			continue
		}
		models = append(models, catwalk.Model{
			ID:                     mid,
			Name:                   mid,
			DefaultMaxTokens:       maxTokens,
			CanReason:              true,
			DefaultReasoningEffort: reasoning,
			ContextWindow:          128000,
		})
	}

	provider := config.ProviderConfig{
		ID:      providerID,
		Name:    cmpTrim(req.Name, providerID),
		BaseURL: baseURL,
		Type:    catwalk.Type(strings.TrimSpace(req.Type)),
		APIKey:  apiKey,
		Models:  models,
	}

	if err := s.workspace.SetConfigField(config.ScopeGlobal, "providers."+providerID, provider); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	// Optionally set as large/small model.
	if req.SetAsLarge && len(models) > 0 {
		_ = s.workspace.UpdatePreferredModel(config.ScopeGlobal, config.SelectedModelTypeLarge, config.SelectedModel{
			Provider:        providerID,
			Model:           models[0].ID,
			MaxTokens:       maxTokens,
			ReasoningEffort: reasoning,
		})
	}
	if req.SetAsSmall && len(models) > 0 {
		_ = s.workspace.UpdatePreferredModel(config.ScopeGlobal, config.SelectedModelTypeSmall, config.SelectedModel{
			Provider: providerID,
			Model:    models[0].ID,
		})
	}

	writeJSON(w, map[string]any{"ok": true, "id": providerID})
}

// handleProviderDelete removes a provider.
func (s *Server) handleProviderDelete(w http.ResponseWriter, r *http.Request) {
	var req ProviderDeleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	providerID := strings.TrimSpace(req.ID)
	if providerID == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("provider id is required"))
		return
	}
	if err := s.workspace.RemoveConfigField(config.ScopeGlobal, "providers."+providerID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// handleConfigExport exports the full config as a downloadable JSON file.
func (s *Server) handleConfigExport(w http.ResponseWriter, r *http.Request) {
	cfg := s.workspace.Config()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename=\"mocode-config-export.json\"")
	_, _ = w.Write(data)
}

// handleConfigImport imports a config JSON (merge or replace mode).
func (s *Server) handleConfigImport(w http.ResponseWriter, r *http.Request) {
	var req ImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("content is required"))
		return
	}

	switch req.Mode {
	case "replace":
		// Replace: write the imported JSON directly to the global config file.
		var imported map[string]any
		if err := json.Unmarshal([]byte(content), &imported); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("invalid JSON: %w", err))
			return
		}
		// Write each top-level key.
		for key, value := range imported {
			if err := s.workspace.SetConfigField(config.ScopeGlobal, key, value); err != nil {
				writeError(w, http.StatusInternalServerError, fmt.Errorf("setting %s: %w", key, err))
				return
			}
		}
	default: // "merge"
		var imported map[string]any
		if err := json.Unmarshal([]byte(content), &imported); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("invalid JSON: %w", err))
			return
		}
		// Merge providers.
		if providers, ok := imported["providers"].(map[string]any); ok {
			for pid, pval := range providers {
				if err := s.workspace.SetConfigField(config.ScopeGlobal, "providers."+pid, pval); err != nil {
					writeError(w, http.StatusInternalServerError, fmt.Errorf("importing provider %s: %w", pid, err))
					return
				}
			}
		}
		// Merge MCP.
		if mcps, ok := imported["mcp"].(map[string]any); ok {
			for mname, mval := range mcps {
				if err := s.workspace.SetConfigField(config.ScopeGlobal, "mcp."+mname, mval); err != nil {
					writeError(w, http.StatusInternalServerError, fmt.Errorf("importing mcp %s: %w", mname, err))
					return
				}
			}
		}
		// Merge options.
		if opts, ok := imported["options"]; ok {
			if err := s.workspace.SetConfigField(config.ScopeGlobal, "options", opts); err != nil {
				writeError(w, http.StatusInternalServerError, fmt.Errorf("importing options: %w", err))
				return
			}
		}
		// Merge agents.
		if agents, ok := imported["agents"].(map[string]any); ok {
			for aid, aval := range agents {
				if err := s.workspace.SetConfigField(config.ScopeGlobal, "agents."+aid, aval); err != nil {
					writeError(w, http.StatusInternalServerError, fmt.Errorf("importing agent %s: %w", aid, err))
					return
				}
			}
		}
	}

	writeJSON(w, map[string]any{"ok": true, "mode": cmpTrim(req.Mode, "merge")})
}

func (s *Server) handleSetConfig(w http.ResponseWriter, r *http.Request) {
	var req ConfigFieldRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.Key) == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("key is required"))
		return
	}
	if err := s.workspace.SetConfigField(req.Scope, req.Key, req.Value); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleProxy(w http.ResponseWriter, r *http.Request) {
	var req ProxyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.workspace.SetConfigField(config.ScopeGlobal, "options.network.enabled", req.Enabled); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := s.workspace.SetConfigField(config.ScopeGlobal, "options.network.proxy_url", req.ProxyURL); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := s.workspace.SetConfigField(config.ScopeGlobal, "options.network.no_proxy", req.NoProxy); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleMiniMaxDefaults(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"provider_id":        "minimax",
		"name":               "MiniMax",
		"type":               string(catwalk.TypeAnthropic),
		"base_url":           "https://api.minimaxi.com/anthropic",
		"quota_base_url":     "https://api.minimaxi.com",
		"default_max_tokens": int64(65536),
		"reasoning_effort":   "medium",
		"active_model":       "MiniMax-M2.7-highspeed",
		"models":             defaultMiniMaxModels(65536, "medium", nil),
	})
}

func (s *Server) handleMiniMaxProvider(w http.ResponseWriter, r *http.Request) {
	var req MiniMaxProviderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	providerID := cmpTrim(req.ProviderID, "minimax")
	if !validConfigName(providerID) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid provider id"))
		return
	}
	baseURL := cmpTrim(req.BaseURL, "https://api.minimaxi.com/anthropic")
	providerType := cmpTrim(req.Type, string(catwalk.TypeAnthropic))
	maxTokens := req.DefaultMaxTokens
	if maxTokens <= 0 {
		maxTokens = 65536
	}
	reasoningEffort := cmpTrim(req.ReasoningEffort, "medium")
	apiKey := strings.TrimSpace(req.APIKey)
	quotaCookie := strings.TrimSpace(req.QuotaCookie)
	if apiKey == "" {
		if current, ok := s.workspace.Config().Providers.Get(providerID); ok {
			apiKey = current.APIKey
			if quotaCookie == "" {
				if current.ProviderOptions != nil {
					if value, ok := current.ProviderOptions["quota_cookie"].(string); ok {
						quotaCookie = value
					}
				}
			}
		}
	}
	provider := config.ProviderConfig{
		ID:      providerID,
		Name:    cmpTrim(req.Name, "MiniMax"),
		BaseURL: baseURL,
		Type:    catwalk.Type(providerType),
		APIKey:  apiKey,
		Models:  defaultMiniMaxModels(maxTokens, reasoningEffort, req.Models),
		ProviderOptions: map[string]any{
			"quota_base_url": cmpTrim(req.QuotaBaseURL, "https://api.minimaxi.com"),
		},
	}
	if quotaCookie != "" {
		provider.ProviderOptions["quota_cookie"] = quotaCookie
	}
	if err := s.workspace.SetConfigField(config.ScopeGlobal, "providers."+providerID, provider); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	activeModel := cmpTrim(req.ActiveModel, "MiniMax-M2.7-highspeed")
	if err := s.workspace.UpdatePreferredModel(config.ScopeGlobal, config.SelectedModelTypeLarge, config.SelectedModel{
		Provider:        providerID,
		Model:           activeModel,
		MaxTokens:       maxTokens,
		ReasoningEffort: reasoningEffort,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "provider": maskedProvider(provider)})
}

func (s *Server) handleMiniMaxQuota(w http.ResponseWriter, r *http.Request) {
	var req MiniMaxProviderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	apiKey := strings.TrimSpace(req.APIKey)
	baseURL := cmpTrim(req.QuotaBaseURL, "https://api.minimaxi.com")
	if apiKey == "" {
		providerID := cmpTrim(req.ProviderID, "minimax")
		if provider, ok := s.workspace.Config().Providers.Get(providerID); ok {
			apiKey = provider.APIKey
			if v, ok := provider.ProviderOptions["quota_base_url"].(string); ok && strings.TrimSpace(v) != "" {
				baseURL = strings.TrimSpace(v)
			}
			if req.QuotaCookie == "" {
				if v, ok := provider.ProviderOptions["quota_cookie"].(string); ok {
					req.QuotaCookie = v
				}
			}
		}
	}
	if apiKey == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("MiniMax API key is required"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	client := minimax.NewMiniMaxQuotaClient(apiKey, baseURL)
	client.SetHTTPClient(s.workspace.Config().HTTPClient(s.workspace.Resolver(), 15*time.Second))
	client.SetCookie(strings.TrimSpace(req.QuotaCookie))
	resp, err := client.FetchQuota(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, resp)
}

func (s *Server) handleMCPServers(w http.ResponseWriter, r *http.Request) {
	states := s.workspace.MCPGetStates()
	servers := make([]map[string]any, 0, len(s.workspace.Config().MCP))
	for _, item := range s.workspace.Config().MCP.Sorted() {
		servers = append(servers, map[string]any{
			"name":  item.Name,
			"mcp":   item.MCP,
			"state": states[item.Name],
			"tools": mcpToolCount(item.Name),
		})
	}
	writeJSON(w, servers)
}

func (s *Server) handleMCPServer(w http.ResponseWriter, r *http.Request) {
	var req MCPServerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	name := strings.TrimSpace(req.Name)
	if !validConfigName(name) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid MCP server name"))
		return
	}
	mcpConfig, err := normalizeMCPConfig(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.workspace.SetConfigField(config.ScopeGlobal, "mcp."+name, mcpConfig); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if !mcpConfig.Disabled {
		_ = s.workspace.EnableMCP(r.Context(), name)
	} else {
		_ = s.workspace.DisableMCP(name)
	}
	writeJSON(w, map[string]any{"ok": true, "name": name, "mcp": mcpConfig})
}

func (s *Server) handleMCPServerDelete(w http.ResponseWriter, r *http.Request) {
	var req MCPServerNameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	name := strings.TrimSpace(req.Name)
	if !validConfigName(name) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid MCP server name"))
		return
	}
	_ = s.workspace.DisableMCP(name)
	if err := s.workspace.RemoveConfigField(config.ScopeGlobal, "mcp."+name); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleMCPServerToggle(w http.ResponseWriter, r *http.Request) {
	var req MCPToggleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	name := strings.TrimSpace(req.Name)
	if !validConfigName(name) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid MCP server name"))
		return
	}
	var err error
	if req.Enable {
		err = s.workspace.EnableMCP(r.Context(), name)
	} else {
		err = s.workspace.DisableMCP(name)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleMCPTools(w http.ResponseWriter, r *http.Request) {
	var tools []ToolInfo
	for server, items := range mcp.Tools() {
		for _, item := range items {
			if item == nil {
				continue
			}
			tools = append(tools, ToolInfo{
				Server:      server,
				Name:        item.Name,
				Description: item.Description,
				InputSchema: item.InputSchema,
			})
		}
	}
	writeJSON(w, tools)
}

func (s *Server) handleWeChatStart(w http.ResponseWriter, r *http.Request) {
	s.wechat.mu.Lock()
	if s.wechat.active {
		state := s.wechat.snapshot()
		s.wechat.mu.Unlock()
		writeJSON(w, state)
		return
	}
	s.wechat.status = "generating"
	s.wechat.qr = ""
	s.wechat.qrImage = ""
	s.wechat.userID = ""
	s.wechat.err = ""
	s.wechat.active = true
	s.wechat.mu.Unlock()
	go s.loginWeChat()
	writeJSON(w, map[string]any{"ok": true, "status": "generating"})
}

func (s *Server) handleWeChatStatus(w http.ResponseWriter, r *http.Request) {
	s.wechat.mu.Lock()
	defer s.wechat.mu.Unlock()
	writeJSON(w, s.wechat.snapshot())
}

func (s *Server) loginWeChat() {
	wc := wechat.Default()
	wc.SetSessionStore(filepath.Join(s.workspace.WorkingDir(), ".mocode", "wechat", "sessions.json"))
	wc.SetAgentHandler(func(ctx context.Context, userID, text string, _ *wechat.IncomingMessage) (string, error) {
		ctx = memory.WithAppUserInContext(ctx, "mocode", "wx:"+userID)
		sessKey := "wx:" + userID
		stopTyping := wc.StartTyping(ctx, userID)
		defer stopTyping()

		if !s.workspace.AgentIsReady() {
			if err := s.workspace.InitCoderAgent(ctx); err != nil {
				return "", fmt.Errorf("agent init: %w", err)
			}
		}

		var sessionID string
		if v, ok := wc.GetSession(sessKey); ok {
			sessionID = v
		}
		if sessionID == "" {
			sess, err := s.workspace.CreateSession(ctx, "WeChat: "+userID)
			if err != nil {
				return "", fmt.Errorf("create session: %w", err)
			}
			sessionID = sess.ID
			wc.SetSession(sessKey, sessionID)
		}

		if err := s.workspace.AgentRun(ctx, sessionID, text); err != nil {
			return "", err
		}
		msgs, err := s.workspace.ListMessages(ctx, sessionID)
		if err != nil || len(msgs) == 0 {
			return "处理完成。", nil
		}
		for i := len(msgs) - 1; i >= 0; i-- {
			if msgs[i].Role == "assistant" {
				return msgs[i].Content().Text, nil
			}
		}
		return "处理完成。", nil
	})
	err := wc.LoginWithCallbacks(context.Background(), true, wechat.LoginCallbacks{
		OnQRURL: func(qrURL string) {
			qr, genErr := wechat.GenerateQR(qrURL)
			s.wechat.mu.Lock()
			defer s.wechat.mu.Unlock()
			if genErr != nil {
				s.wechat.status = "error"
				s.wechat.err = genErr.Error()
				s.wechat.active = false
				return
			}
			s.wechat.status = "scan"
			s.wechat.qr = qr.ASCII
			s.wechat.qrImage = qr.PNGDataURL
		},
		OnScanned: func() {
			s.wechat.mu.Lock()
			defer s.wechat.mu.Unlock()
			s.wechat.status = "scanned"
		},
		OnExpired: func() {
			s.wechat.mu.Lock()
			defer s.wechat.mu.Unlock()
			s.wechat.status = "expired"
		},
		OnLoggedIn: func(userID string) {
			s.wechat.mu.Lock()
			defer s.wechat.mu.Unlock()
			s.wechat.status = "connected"
			s.wechat.userID = userID
		},
	}, s.workspace.Config().HTTPClient(s.workspace.Resolver(), 45*time.Second))
	s.wechat.mu.Lock()
	defer s.wechat.mu.Unlock()
	if err != nil {
		s.wechat.status = "error"
		s.wechat.err = err.Error()
		s.wechat.active = false
		return
	}
	s.wechat.status = "connected"
	if wc.Credentials != nil {
		s.wechat.userID = wc.Credentials.UserID
	}
	s.wechat.active = false
	// Initialize butler routing on login.
	wc.InitButler(&adminButlerWorkspace{s.workspace})
	go func() {
		_ = wc.Run(context.Background())
	}()
}

func (w *wechatState) snapshot() map[string]any {
	return map[string]any{
		"status":   w.status,
		"qr":       w.qr,
		"qr_image": w.qrImage,
		"user_id":  w.userID,
		"error":    w.err,
		"active":   w.active,
	}
}

func assetSubFS() fs.FS {
	sub, err := fs.Sub(assetsFS, "assets")
	if err != nil {
		return assetsFS
	}
	return sub
}

func defaultMiniMaxModels(maxTokens int64, reasoningEffort string, ids []string) []catwalk.Model {
	if maxTokens <= 0 {
		maxTokens = 65536
	}
	reasoningEffort = cmpTrim(reasoningEffort, "medium")
	if len(ids) == 0 {
		ids = []string{"MiniMax-M2.7-highspeed", "MiniMax-M2.7"}
	}
	models := make([]catwalk.Model, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		models = append(models, catwalk.Model{
			ID:                     id,
			Name:                   miniMaxModelName(id),
			ContextWindow:          204800,
			DefaultMaxTokens:       maxTokens,
			CanReason:              true,
			ReasoningLevels:        []string{"low", "medium", "high"},
			DefaultReasoningEffort: reasoningEffort,
			SupportsImages:         true,
		})
	}
	return models
}

func miniMaxModelName(id string) string {
	switch id {
	case "MiniMax-M2.7-highspeed":
		return "MiniMax M2.7 Highspeed"
	case "MiniMax-M2.7":
		return "MiniMax M2.7"
	case "MiniMax-M2.5":
		return "MiniMax M2.5"
	default:
		return strings.ReplaceAll(id, "-", " ")
	}
}

func normalizeMCPConfig(req MCPServerRequest) (config.MCPConfig, error) {
	mcpType := config.MCPType(strings.ToLower(strings.TrimSpace(string(req.Type))))
	if mcpType == "" {
		mcpType = config.MCPStdio
	}
	cfg := config.MCPConfig{
		Type:          mcpType,
		Timeout:       req.Timeout,
		Disabled:      req.Disabled,
		DisabledTools: cleanList(req.DisabledTools),
	}
	switch mcpType {
	case config.MCPStdio:
		cfg.Command = strings.TrimSpace(req.Command)
		if cfg.Command == "" {
			return cfg, fmt.Errorf("command is required for stdio MCP")
		}
		cfg.Args = cleanList(req.Args)
		cfg.Env = cleanMap(req.Env)
	case config.MCPHttp, config.MCPSSE:
		cfg.URL = strings.TrimSpace(req.URL)
		if cfg.URL == "" {
			return cfg, fmt.Errorf("url is required for %s MCP", mcpType)
		}
		cfg.Headers = cleanMap(req.Headers)
	default:
		return cfg, fmt.Errorf("unsupported MCP type %q", mcpType)
	}
	return cfg, nil
}

func validConfigName(name string) bool {
	if name == "" || strings.Contains(name, ".") {
		return false
	}
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

func cleanList(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

func cleanMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	result := make(map[string]string, len(values))
	for key, value := range values {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			result[key] = value
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func cmpTrim(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func maskedProvider(provider config.ProviderConfig) config.ProviderConfig {
	if provider.APIKey != "" {
		provider.APIKey = maskSecret(provider.APIKey)
	}
	if cookie, ok := provider.ProviderOptions["quota_cookie"].(string); ok && cookie != "" {
		provider.ProviderOptions["quota_cookie"] = maskSecret(cookie)
	}
	return provider
}

func maskSecret(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 10 {
		return "••••"
	}
	return value[:4] + "••••" + value[len(value)-4:]
}

func mcpToolCount(name string) int {
	for server, tools := range mcp.Tools() {
		if server == name {
			return len(tools)
		}
	}
	return 0
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
}

// adminButlerWorkspace adapts workspace.Workspace to wechat.ButlerWorkspace.
type adminButlerWorkspace struct {
	ws workspace.Workspace
}

func (w *adminButlerWorkspace) CreateSession(ctx context.Context, title string) (string, error) {
	sess, err := w.ws.CreateSession(ctx, title)
	if err != nil {
		return "", err
	}
	return sess.ID, nil
}

func (w *adminButlerWorkspace) GetSession(ctx context.Context, id string) (string, error) {
	sess, err := w.ws.GetSession(ctx, id)
	if err != nil {
		return "", err
	}
	return sess.Title, nil
}

func (w *adminButlerWorkspace) ListSessions(ctx context.Context) ([]wechat.SessionInfo, error) {
	sessions, err := w.ws.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]wechat.SessionInfo, len(sessions))
	for i, s := range sessions {
		result[i] = wechat.SessionInfo{
			ID:        s.ID,
			Title:     s.Title,
			CreatedAt: time.Unix(s.CreatedAt, 0).Format("2006-01-02 15:04"),
		}
	}
	return result, nil
}

func (w *adminButlerWorkspace) DeleteSession(ctx context.Context, id string) error {
	return w.ws.DeleteSession(ctx, id)
}

func (w *adminButlerWorkspace) AgentRun(ctx context.Context, id, prompt string) error {
	return w.ws.AgentRun(ctx, id, prompt)
}

func (w *adminButlerWorkspace) ListMessages(ctx context.Context, id string) ([]wechat.MsgInfo, error) {
	msgs, err := w.ws.ListMessages(ctx, id)
	if err != nil {
		return nil, err
	}
	result := make([]wechat.MsgInfo, 0, len(msgs))
	for _, m := range msgs {
		result = append(result, wechat.MsgInfo{Role: string(m.Role), Content: m.Content().Text})
	}
	return result, nil
}

func (w *adminButlerWorkspace) AgentIsSessionBusy(_ context.Context, sessionID string) bool {
	return w.ws.AgentIsSessionBusy(sessionID)
}

func (w *adminButlerWorkspace) CurrentModel() string {
	cfg := w.ws.Config()
	if large, ok := cfg.Models[config.SelectedModelTypeLarge]; ok {
		return large.Provider + "/" + large.Model
	}
	return ""
}

func (w *adminButlerWorkspace) UpdateModel(provider, model string) error {
	return w.ws.UpdatePreferredModel(config.ScopeGlobal, config.SelectedModelTypeLarge, config.SelectedModel{
		Provider: provider,
		Model:    model,
	})
}
