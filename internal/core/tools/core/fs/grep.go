package fs

import (
	"cmp"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/core/tools/contract"
	searchcommon "github.com/nextsko/mocode-agent/internal/core/tools/external/common/search"

	"github.com/nextsko/mocode-agent/internal/core/config"
)

// ResetCache clears compiled regex caches to prevent unbounded growth across sessions.
// The caches themselves live in plugins/searchcommon since the search-stack
// promotion; this root alias keeps the historical tools.ResetCache export.
func ResetCache() {
	searchcommon.ResetRegexCaches()
}

type GrepParams struct {
	Pattern     string `json:"pattern" description:"The regex pattern to search for in file contents"`
	Path        string `json:"path,omitempty" description:"The directory to search in. Defaults to the current working directory."`
	Include     string `json:"include,omitempty" description:"File pattern to include in the search (e.g. \"*.js\", \"*.{ts,tsx}\")"`
	LiteralText bool   `json:"literal_text,omitempty" description:"If true, the pattern will be treated as literal text with special regex characters escaped. Default is false."`
}

type GrepMatch = searchcommon.Match

type GrepResponseMetadata struct {
	NumberOfMatches int  `json:"number_of_matches"`
	Truncated       bool `json:"truncated"`
}

const (
	GrepToolName        = "grep"
	maxGrepContentWidth = 500
)

//go:embed grep.md
var grepDescription []byte

// escapeRegexPattern escapes special regex characters so they're treated as literal characters.
func escapeRegexPattern(pattern string) string {
	return searchcommon.EscapeRegexPattern(pattern)
}

// NewGrepPortTool is the framework-agnostic form of the grep tool.
func NewGrepPortTool(workingDir string, config config.ToolGrep) contract.Tool {
	return contract.NewFunc(
		GrepToolName,
		toolutil.FirstLineDescription(grepDescription),
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{
					"type":        "string",
					"description": "The regex pattern to search for in file contents",
				},
				"path": map[string]any{
					"type":        "string",
					"description": "The directory to search in. Defaults to the current working directory.",
				},
				"include": map[string]any{
					"type":        "string",
					"description": `File pattern to include in the search (e.g. "*.js", "*.{ts,tsx}")`,
				},
				"literal_text": map[string]any{
					"type":        "boolean",
					"description": "If true, the pattern will be treated as literal text with special regex characters escaped. Default is false.",
				},
			},
			"required": []string{"pattern"},
		},
		func(ctx context.Context, _ contract.ToolContext, args json.RawMessage) (contract.ToolResult, error) {
			var params GrepParams
			if err := json.Unmarshal(args, &params); err != nil {
				return contract.ToolResult{}, fmt.Errorf("invalid parameters: %w", err)
			}
			return runGrep(ctx, workingDir, config, params)
		},
	)
}

// runGrep searches file contents and renders the matches.
func runGrep(ctx context.Context, workingDir string, config config.ToolGrep, params GrepParams) (contract.ToolResult, error) {
	if params.Pattern == "" {
		return contract.Error("pattern is required"), nil
	}

	searchPattern := params.Pattern
	if params.LiteralText {
		searchPattern = escapeRegexPattern(params.Pattern)
	}

	searchPath := cmp.Or(params.Path, workingDir)

	searchCtx, cancel := context.WithTimeout(ctx, config.GetTimeout())
	defer cancel()

	matches, truncated, err := SearchFiles(searchCtx, searchPattern, searchPath, params.Include, 100)
	if err != nil {
		return contract.Error(fmt.Sprintf("error searching files: %v", err)), nil
	}

	var output strings.Builder
	if len(matches) == 0 { //nolint:nestif // extracted verbatim from the legacy closure.
		output.WriteString("No files found")
	} else {
		fmt.Fprintf(&output, "Found %d matches\n", len(matches))

		currentFile := ""
		for _, match := range matches {
			if currentFile != match.Path {
				if currentFile != "" {
					output.WriteString("\n")
				}
				currentFile = match.Path
				fmt.Fprintf(&output, "%s:\n", filepath.ToSlash(match.Path))
			}
			if match.LineNum > 0 {
				lineText := match.LineText
				if len(lineText) > maxGrepContentWidth {
					lineText = lineText[:maxGrepContentWidth] + "..."
				}
				if match.CharNum > 0 {
					fmt.Fprintf(&output, "  Line %d, Char %d: %s\n", match.LineNum, match.CharNum, lineText)
				} else {
					fmt.Fprintf(&output, "  Line %d: %s\n", match.LineNum, lineText)
				}
			} else {
				fmt.Fprintf(&output, "  %s\n", match.Path)
			}
		}

		if truncated {
			output.WriteString("\n(Results are truncated. Consider using a more specific path or pattern.)")
		}
	}

	return contract.WithMetadata(contract.Text(output.String()), GrepResponseMetadata{
		NumberOfMatches: len(matches),
		Truncated:       truncated,
	}), nil
}

func SearchFiles(ctx context.Context, pattern, rootPath, include string, limit int) ([]GrepMatch, bool, error) {
	return searchcommon.SearchFiles(ctx, pattern, rootPath, include, limit)
}
