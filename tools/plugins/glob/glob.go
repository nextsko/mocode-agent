package glob

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/package-register/mocode/internal/util/fsext"
	"github.com/package-register/mocode/tools"
	"github.com/package-register/mocode/tools/plugins/searchcommon"
)

const GlobToolName = "glob"

//go:embed glob.md
var globDescription []byte

type GlobParams struct {
	Pattern string `json:"pattern" description:"The glob pattern to match files against"`
	Path    string `json:"path,omitempty" description:"The directory to search in. Defaults to the current working directory."`
}

type GlobResponseMetadata struct {
	NumberOfFiles int  `json:"number_of_files"`
	Truncated     bool `json:"truncated"`
}

type globTool struct{}

func New() tools.Tool { return &globTool{} }

func (t *globTool) Name() string { return GlobToolName }

func (t *globTool) Description() string { return firstLine(globDescription) }

func (t *globTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        GlobToolName,
		Description: t.Description(),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{
					"type":        "string",
					"description": "The glob pattern to match files against",
				},
				"path": map[string]any{
					"type":        "string",
					"description": "The directory to search in. Defaults to the current working directory.",
				},
			},
			"required": []string{"pattern"},
		},
	}
}

func (t *globTool) Execute(ctx context.Context, tctx tools.ToolContext, args json.RawMessage) (tools.ToolResult, error) {
	var params GlobParams
	if err := json.Unmarshal(args, &params); err != nil {
		return tools.ErrorResult(err)
	}
	if params.Pattern == "" {
		return tools.ToolResult{Error: errors.New("pattern is required")}, nil
	}

	searchPath := params.Path
	if searchPath == "" && tctx != nil {
		searchPath = tctx.WorkingDir()
	}
	if searchPath == "" {
		searchPath = "."
	}

	files, truncated, err := GlobFiles(ctx, params.Pattern, searchPath, 100)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("error finding files: %w", err)
	}

	output := "No files found"
	if len(files) > 0 {
		normalizeFilePaths(files)
		output = strings.Join(files, "\n")
		if truncated {
			output += "\n\n(Results are truncated. Consider using a more specific path or pattern.)"
		}
	}

	return tools.ToolResult{
		Content: output,
		Metadata: map[string]any{
			"number_of_files": len(files),
			"truncated":       truncated,
		},
	}, nil
}

func GlobFiles(ctx context.Context, pattern, searchPath string, limit int) ([]string, bool, error) {
	cmdRg := searchcommon.GetRgCmd(ctx, pattern)
	if cmdRg != nil {
		cmdRg.Dir = searchPath
		matches, err := searchcommon.RunRipgrep(cmdRg, searchPath, limit)
		if err == nil {
			return matches, len(matches) >= limit && limit > 0, nil
		}
		slog.Warn("Ripgrep execution failed, falling back to doublestar", "error", err)
	}

	return fsext.GlobGitignoreAware(pattern, searchPath, limit)
}

func normalizeFilePaths(paths []string) {
	for i, p := range paths {
		paths[i] = filepath.ToSlash(p)
	}
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

func init() { tools.Register(GlobToolName, New()) }
