package fs

import (
	"cmp"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/core/tools/contract"
	searchcommon "github.com/nextsko/mocode-agent/internal/core/tools/external/common/search"

	"github.com/nextsko/mocode-agent/internal/util/fsext"
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

// NewGlobPortTool is the framework-agnostic form of the glob tool.
func NewGlobPortTool(workingDir string) contract.Tool {
	return contract.NewFunc(
		GlobToolName,
		toolutil.FirstLineDescription(globDescription),
		map[string]any{
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
		func(ctx context.Context, _ contract.ToolContext, args json.RawMessage) (contract.ToolResult, error) {
			var params GlobParams
			if err := json.Unmarshal(args, &params); err != nil {
				return contract.ToolResult{}, fmt.Errorf("invalid parameters: %w", err)
			}
			return runGlob(ctx, workingDir, params)
		},
	)
}

// runGlob expands the pattern and renders the matched file list.
func runGlob(ctx context.Context, workingDir string, params GlobParams) (contract.ToolResult, error) {
	if params.Pattern == "" {
		return contract.Error("pattern is required"), nil
	}

	searchPath := cmp.Or(params.Path, workingDir)

	files, truncated, err := globFiles(ctx, params.Pattern, searchPath, 100)
	if err != nil {
		return contract.ToolResult{}, fmt.Errorf("error finding files: %w", err)
	}

	var output string
	if len(files) == 0 {
		output = "No files found"
	} else {
		normalizeFilePaths(files)
		output = strings.Join(files, "\n")
		if truncated {
			output += "\n\n(Results are truncated. Consider using a more specific path or pattern.)"
		}
	}

	return contract.WithMetadata(contract.Text(output), GlobResponseMetadata{
		NumberOfFiles: len(files),
		Truncated:     truncated,
	}), nil
}

func globFiles(ctx context.Context, pattern, searchPath string, limit int) ([]string, bool, error) {
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
