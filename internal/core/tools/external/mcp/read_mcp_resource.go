package mcp

import (
	"cmp"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/core/config"
	"github.com/nextsko/mocode-agent/internal/core/permission"
	"github.com/nextsko/mocode-agent/internal/core/tools/contract"
	"github.com/nextsko/mocode-agent/internal/util/ext"
)

type ReadMCPResourceParams struct {
	MCPName string `json:"mcp_name" description:"The MCP server name"`
	URI     string `json:"uri" description:"The resource URI to read"`
}

type ReadMCPResourcePermissionsParams struct {
	MCPName string `json:"mcp_name"`
	URI     string `json:"uri"`
}

const ReadMCPResourceToolName = "read_mcp_resource"

//go:embed read_mcp_resource.md
var readMCPResourceDescription []byte

// NewReadMCPResourcePortTool is the framework-agnostic form of the
// read_mcp_resource tool. It is marked parallel-safe like the legacy
// fantasy.NewParallelAgentTool constructor it replaces.
func NewReadMCPResourcePortTool(cfg *config.ConfigStore, permissions permission.Service) contract.Tool {
	return asParallel(contract.NewFunc(
		ReadMCPResourceToolName,
		toolutil.FirstLineDescription(readMCPResourceDescription),
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"mcp_name": map[string]any{
					"type":        "string",
					"description": "The MCP server name",
				},
				"uri": map[string]any{
					"type":        "string",
					"description": "The resource URI to read",
				},
			},
			"required": []string{"mcp_name", "uri"},
		},
		func(ctx context.Context, _ contract.ToolContext, args json.RawMessage) (contract.ToolResult, error) {
			var params ReadMCPResourceParams
			if len(args) > 0 {
				if err := json.Unmarshal(args, &params); err != nil {
					return contract.ToolResult{}, fmt.Errorf("invalid parameters: %w", err)
				}
			}
			callID, _ := toolutil.ToolCallIDFromCtx(ctx)
			return runReadMCPResource(ctx, cfg, permissions, callID, params)
		},
	))
}

// runReadMCPResource reads one resource's text/blob content.
func runReadMCPResource(ctx context.Context, cfg *config.ConfigStore, permissions permission.Service, callID string, params ReadMCPResourceParams) (contract.ToolResult, error) {
	params.MCPName = strings.TrimSpace(params.MCPName)
	params.URI = strings.TrimSpace(params.URI)
	if params.MCPName == "" {
		return contract.Error("mcp_name parameter is required"), nil
	}
	if params.URI == "" {
		return contract.Error("uri parameter is required"), nil
	}

	sessionID := toolutil.GetSessionFromContext(ctx)
	if sessionID == "" {
		return contract.ToolResult{}, fmt.Errorf("session ID is required for reading MCP resources")
	}

	relPath := ext.SmartJoin(cfg.WorkingDir(), cmp.Or(params.URI, "mcp-resource"))
	p, err := permissions.Request(ctx,
		permission.CreatePermissionRequest{
			SessionID:   sessionID,
			Path:        relPath,
			ToolCallID:  callID,
			ToolName:    ReadMCPResourceToolName,
			Action:      "read",
			Description: fmt.Sprintf("Read MCP resource from %s", params.MCPName),
			Params:      ReadMCPResourcePermissionsParams(params),
		},
	)
	if err != nil {
		return contract.ToolResult{}, err
	}
	if !p {
		return permissionDeniedResult(), nil
	}

	contents, err := ReadResource(ctx, cfg, params.MCPName, params.URI)
	if err != nil {
		return contract.Error(err.Error()), nil
	}
	if len(contents) == 0 {
		return contract.Text(""), nil
	}

	var textParts []string
	for _, content := range contents {
		if content == nil {
			continue
		}
		if content.Text != "" {
			textParts = append(textParts, content.Text)
			continue
		}
		if len(content.Blob) > 0 {
			textParts = append(textParts, string(content.Blob))
			continue
		}
		slog.Debug("MCP resource content missing text/blob", "uri", content.URI)
	}

	if len(textParts) == 0 {
		return contract.Text(""), nil
	}

	return contract.Text(strings.Join(textParts, "\n")), nil
}
