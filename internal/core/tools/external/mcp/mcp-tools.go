package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/core/config"
	"github.com/nextsko/mocode-agent/internal/core/permission"
	"github.com/nextsko/mocode-agent/internal/core/tools/contract"
)

// whitelistDockerTools contains Docker MCP tools that don't require permission.
var whitelistDockerTools = []string{
	"mcp_docker_mcp-find",
	"mcp_docker_mcp-add",
	"mcp_docker_mcp-remove",
	"mcp_docker_mcp-config-set",
	"mcp_docker_code-mode",
}

// GetMCPTools gets all the currently available MCP tools.
func GetMCPTools(permissions permission.Service, cfg *config.ConfigStore, wd string) []*MCPToolStruct {
	var result []*MCPToolStruct
	for mcpName, tools := range Tools() {
		for _, tool := range tools {
			result = append(result, &MCPToolStruct{
				mcpName:     mcpName,
				tool:        tool,
				permissions: permissions,
				workingDir:  wd,
				cfg:         cfg,
			})
		}
	}
	return result
}

// Tool is a tool from a MCP.
type MCPToolStruct struct {
	mcpName     string
	tool        *Tool
	cfg         *config.ConfigStore
	permissions permission.Service
	workingDir  string
}

// Description implements contract.Tool.
func (m *MCPToolStruct) Description() string { return m.tool.Description }

// MCP returns the MCP server name this tool belongs to.
func (m *MCPToolStruct) MCP() string { return m.mcpName }

// MCPToolName returns the original tool name inside the MCP server.
func (m *MCPToolStruct) MCPToolName() string { return m.tool.Name }

// Name implements contract.Tool.
func (m *MCPToolStruct) Name() string { return fmt.Sprintf("mcp_%s_%s", m.mcpName, m.tool.Name) }

// Schema implements contract.Tool, forwarding the MCP server's JSON Schema.
func (m *MCPToolStruct) Schema() contract.Schema {
	parameters := make(map[string]any)
	if input, ok := m.tool.InputSchema.(map[string]any); ok {
		parameters = input
	}
	return contract.Schema{
		Name:        m.Name(),
		Description: m.tool.Description,
		Parameters:  parameters,
	}
}

// Execute implements contract.Tool.
func (m *MCPToolStruct) Execute(ctx context.Context, _ contract.ToolContext, args json.RawMessage) (contract.ToolResult, error) {
	sessionID := toolutil.GetSessionFromContext(ctx)
	if sessionID == "" {
		return contract.ToolResult{}, fmt.Errorf("session ID is required for creating a new file")
	}

	// Skip permission for whitelisted Docker MCP tools.
	if !slices.Contains(whitelistDockerTools, m.Name()) {
		permissionDescription := fmt.Sprintf("execute %s with the following parameters:", m.Name())
		callID, _ := toolutil.ToolCallIDFromCtx(ctx)
		p, err := m.permissions.Request(ctx,
			permission.CreatePermissionRequest{
				SessionID:   sessionID,
				ToolCallID:  callID,
				Path:        m.workingDir,
				ToolName:    m.Name(),
				Action:      "execute",
				Description: permissionDescription,
				Params:      string(args),
			},
		)
		if err != nil {
			return contract.ToolResult{}, err
		}
		if !p {
			return contract.Error("User denied permission"), nil
		}
	}

	result, err := RunTool(ctx, m.cfg, m.mcpName, m.tool.Name, string(args))
	if err != nil {
		return contract.Error(err.Error()), nil
	}

	switch result.Type {
	case "image", "media":
		if !toolutil.GetSupportsImagesFromContext(ctx) {
			modelName := toolutil.GetModelNameFromContext(ctx)
			return contract.Error(fmt.Sprintf("This model (%s) does not support image data.", modelName)), nil
		}
		return contract.ToolResult{
			Content: result.Content,
			Attachments: []contract.Attachment{{
				MimeType: result.MediaType,
				Content:  result.Data,
			}},
		}, nil
	default:
		return contract.Text(result.Content), nil
	}
}
