package agent

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"charm.land/fantasy"
	"github.com/nextsko/mocode-agent/internal/core/config"
)

func (c *coordinator) Cancel(sessionID string) {
	c.currentAgent.Cancel(sessionID)
}

// CancelSubagent stops a single sub-agent dispatched by the Agent tool,
// identified by its user-visible subagentID (params.AgentID, e.g.
// "<parentToolCallID>-1"). The parent session is left running.
//
// The mapping from subagentID to internal sub-session ID is maintained by
// runSubAgentWithMeta and removed in a defer. If the subagentID is
// unknown (already completed, never registered, or never ran in this
// process) the call is a silent no-op — that matches the behaviour of
// sessionAgent.Cancel for arbitrary session IDs.
func (c *coordinator) CancelSubagent(subagentID string) {
	if subagentID == "" || c.subagentIndex == nil {
		return
	}
	subSessionID, ok := c.subagentIndex.Take(subagentID)
	if !ok || subSessionID == "" {
		return
	}
	c.currentAgent.Cancel(subSessionID)
}

func (c *coordinator) CancelAll() {
	c.currentAgent.CancelAll()
}

// Close drains the coordinator's tool registry, closing plugin-owned
// resources such as the shared SSH connection pool. Safe to call once at
// shutdown; Build after Close recreates lazily-startable state.
func (c *coordinator) Close(ctx context.Context) error {
	if c.toolRegistry == nil {
		return nil
	}
	if err := c.toolRegistry.StopAll(ctx); err != nil {
		slog.Warn("tool registry shutdown reported error", "error", err)
		return err
	}
	return nil
}

func (c *coordinator) ClearQueue(sessionID string) {
	c.currentAgent.ClearQueue(sessionID)
}

func (c *coordinator) IsBusy() bool {
	return c.currentAgent.IsBusy()
}

func (c *coordinator) IsSessionBusy(sessionID string) bool {
	return c.currentAgent.IsSessionBusy(sessionID)
}

func (c *coordinator) isUnauthorized(err error) bool {
	var providerErr *fantasy.ProviderError
	return errors.As(err, &providerErr) && providerErr.StatusCode == http.StatusUnauthorized
}

func (c *coordinator) refreshOAuth2Token(ctx context.Context, providerCfg config.ProviderConfig) error {
	if err := c.cfg.RefreshOAuthToken(ctx, config.ScopeGlobal, providerCfg.ID); err != nil {
		slog.Error("Failed to refresh OAuth token after 401 error", "provider", providerCfg.ID, "error", err)
		return err
	}
	if err := c.UpdateModels(ctx); err != nil {
		return err
	}
	return nil
}

func (c *coordinator) refreshApiKeyTemplate(ctx context.Context, providerCfg config.ProviderConfig) error {
	newAPIKey, err := c.cfg.Resolve(providerCfg.APIKeyTemplate)
	if err != nil {
		slog.Error("Failed to re-resolve API key after 401 error", "provider", providerCfg.ID, "error", err)
		return err
	}

	providerCfg.APIKey = newAPIKey
	c.cfg.Config().Providers.Set(providerCfg.ID, providerCfg)

	if err := c.UpdateModels(ctx); err != nil {
		return err
	}
	return nil
}
