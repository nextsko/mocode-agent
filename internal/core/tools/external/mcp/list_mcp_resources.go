package mcp

import (
	"cmp"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/core/config"
	"github.com/nextsko/mocode-agent/internal/core/permission"
	"github.com/nextsko/mocode-agent/internal/core/tools/contract"
	"github.com/nextsko/mocode-agent/internal/util/ext"
)

type ListMCPResourcesParams struct {
	MCPName string `json:"mcp_name" description:"The MCP server name"`
}

type ListMCPResourcesPermissionsParams struct {
	MCPName string `json:"mcp_name"`
}

const ListMCPResourcesToolName = "list_mcp_resources"

//go:embed list_mcp_resources.md
var listMCPResourcesDescription []byte

// NewListMCPResourcesPortTool is the framework-agnostic form of the
// list_mcp_resources tool. It is marked parallel-safe like the legacy
// fantasy.NewParallelAgentTool constructor it replaces.
func NewListMCPResourcesPortTool(cfg *config.ConfigStore, permissions permission.Service) contract.Tool {
	return asParallel(contract.NewFunc(
		ListMCPResourcesToolName,
		toolutil.FirstLineDescription(listMCPResourcesDescription),
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"mcp_name": map[string]any{
					"type":        "string",
					"description": "The MCP server name",
				},
			},
			"required": []string{"mcp_name"},
		},
		func(ctx context.Context, _ contract.ToolContext, args json.RawMessage) (contract.ToolResult, error) {
			var params ListMCPResourcesParams
			if len(args) > 0 {
				if err := json.Unmarshal(args, &params); err != nil {
					return contract.ToolResult{}, fmt.Errorf("invalid parameters: %w", err)
				}
			}
			callID, _ := toolutil.ToolCallIDFromCtx(ctx)
			return runListMCPResources(ctx, cfg, permissions, callID, params)
		},
	))
}

// runListMCPResources lists the resources one MCP server exposes.
func runListMCPResources(ctx context.Context, cfg *config.ConfigStore, permissions permission.Service, callID string, params ListMCPResourcesParams) (contract.ToolResult, error) {
	params.MCPName = strings.TrimSpace(params.MCPName)
	if params.MCPName == "" {
		return contract.Error("mcp_name parameter is required"), nil
	}

	sessionID := toolutil.GetSessionFromContext(ctx)
	if sessionID == "" {
		return contract.ToolResult{}, fmt.Errorf("session ID is required for listing MCP resources")
	}

	relPath := ext.SmartJoin(cfg.WorkingDir(), params.MCPName)
	p, err := permissions.Request(ctx,
		permission.CreatePermissionRequest{
			SessionID:   sessionID,
			Path:        relPath,
			ToolCallID:  callID,
			ToolName:    ListMCPResourcesToolName,
			Action:      "list",
			Description: fmt.Sprintf("List MCP resources from %s", params.MCPName),
			Params:      ListMCPResourcesPermissionsParams(params),
		},
	)
	if err != nil {
		return contract.ToolResult{}, err
	}
	if !p {
		return permissionDeniedResult(), nil
	}

	resources, err := ListResources(ctx, cfg, params.MCPName)
	if err != nil {
		return contract.Error(err.Error()), nil
	}
	if len(resources) == 0 {
		return contract.Text("No resources found"), nil
	}

	lines := make([]string, 0, len(resources))
	for _, resource := range resources {
		if resource == nil {
			continue
		}
		title := cmp.Or(resource.Title, resource.Name, resource.URI)
		line := fmt.Sprintf("- %s", title)
		if resource.URI != "" {
			line = fmt.Sprintf("%s (%s)", line, resource.URI)
		}
		if resource.Description != "" {
			line = fmt.Sprintf("%s: %s", line, resource.Description)
		}
		if resource.MIMEType != "" {
			line = fmt.Sprintf("%s [mime: %s]", line, resource.MIMEType)
		}
		if resource.Size > 0 {
			line = fmt.Sprintf("%s [size: %d]", line, resource.Size)
		}
		lines = append(lines, line)
	}

	sort.Strings(lines)
	return contract.Text(strings.Join(lines, "\n")), nil
}
