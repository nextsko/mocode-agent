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
	"time"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/core/tools/contract"
	searchcommon "github.com/nextsko/mocode-agent/internal/core/tools/external/common/search"

	"github.com/nextsko/mocode-agent/internal/util/fsext"
)

const GlobToolName = "glob"

// globScanTimeout caps one glob call. An unbounded scan of a huge mount was
// the worst latency path in the zxxw deployment trace (7.7 min on /data01);
// a legitimate search that needs longer should narrow the path, not wait
// longer. Var (not const) so tests can shrink it.
var globScanTimeout = 60 * time.Second

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

	globCtx, cancel := context.WithTimeout(ctx, globScanTimeout)
	defer cancel()

	files, truncated, err := globFiles(globCtx, params.Pattern, searchPath, 100)
	if err != nil {
		return contract.ToolResult{}, fmt.Errorf("error finding files: %w", err)
	}
	if globCtx.Err() != nil {
		truncated = true
	}

	var output string
	if len(files) == 0 {
		output = "No files found. " + globNarrowHint(searchPath, globCtx)
	} else {
		normalizeFilePaths(files)
		output = strings.Join(files, "\n")
		if truncated {
			output += "\n\n(Results are truncated. " + globNarrowHint(searchPath, globCtx) + ")"
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
		matches, stoppedEarly, err := searchcommon.RunRipgrep(cmdRg, searchPath, limit)
		if err == nil {
			return matches, stoppedEarly || len(matches) >= limit, nil
		}
		if ctx.Err() != nil {
			// Deadline hit: return what ripgrep already streamed instead of
			// re-walking the whole tree with the fallback — doubling the
			// cost of every timeout was exactly the zxxw failure mode.
			return matches, true, nil
		}
		slog.Warn("Ripgrep execution failed, falling back to doublestar", "error", err)
	}

	return fsext.GlobGitignoreAware(pattern, searchPath, limit)
}

// globNarrowHint tells the model how to recover from a truncated or timed
// out scan instead of retrying the same call.
func globNarrowHint(searchPath string, ctx context.Context) string {
	if ctx.Err() != nil {
		return fmt.Sprintf(
			"The scan hit the %v limit and was stopped early — narrow the path (current: %s) or use a more specific pattern instead of retrying the same call.",
			globScanTimeout, searchPath,
		)
	}
	return "Consider using a more specific path or pattern."
}

func normalizeFilePaths(paths []string) {
	for i, p := range paths {
		paths[i] = filepath.ToSlash(p)
	}
}
