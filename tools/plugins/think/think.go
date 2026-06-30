package think

import (
	"context"
	_ "embed"
	"encoding/json"
	"strings"

	"github.com/package-register/mocode/tools"
)

//go:embed think.md
var thinkDescription []byte

const ThinkToolName = "think"

type ThinkParams struct {
	Thought string `json:"thought" description:"The reasoning or analysis to record before acting"`
}

type ThinkResponseMetadata struct {
	Thought string `json:"thought"`
}

type thinkTool struct{}

func New() tools.Tool { return &thinkTool{} }

func (t *thinkTool) Name() string { return ThinkToolName }

func (t *thinkTool) Description() string { return firstLine(thinkDescription) }

func (t *thinkTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        ThinkToolName,
		Description: t.Description(),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"thought": map[string]any{
					"type":        "string",
					"description": "The reasoning or analysis to record before acting",
				},
			},
			"required": []string{"thought"},
		},
	}
}

func (t *thinkTool) Execute(_ context.Context, _ tools.ToolContext, args json.RawMessage) (tools.ToolResult, error) {
	var params ThinkParams
	if err := json.Unmarshal(args, &params); err != nil {
		return tools.ErrorResult(err)
	}
	return tools.ToolResult{
		Content: params.Thought,
		Metadata: map[string]any{
			"thought": params.Thought,
		},
	}, nil
}

func firstLine(content []byte) string {
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

func init() { tools.Register(ThinkToolName, New()) }
