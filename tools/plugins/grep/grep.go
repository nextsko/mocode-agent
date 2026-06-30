package grep

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/package-register/mocode/internal/util/fsext"
	"github.com/package-register/mocode/tools"
	"github.com/package-register/mocode/tools/plugins/searchcommon"
)

const (
	GrepToolName        = "grep"
	maxGrepContentWidth = 500
)

//go:embed grep.md
var grepDescription []byte

var globBraceRegex = regexp.MustCompile(`\{([^}]+)\}`)

type GrepParams struct {
	Pattern     string `json:"pattern" description:"The regex pattern to search for in file contents"`
	Path        string `json:"path,omitempty" description:"The directory to search in. Defaults to the current working directory."`
	Include     string `json:"include,omitempty" description:"File pattern to include in the search (e.g. \"*.js\", \"*.{ts,tsx}\")"`
	LiteralText bool   `json:"literal_text,omitempty" description:"If true, the pattern will be treated as literal text with special regex characters escaped. Default is false."`
}

type GrepMatch struct {
	Path     string
	ModTime  time.Time
	LineNum  int
	CharNum  int
	LineText string
}

type GrepResponseMetadata struct {
	NumberOfMatches int  `json:"number_of_matches"`
	Truncated       bool `json:"truncated"`
}

type grepTool struct{}

func New() tools.Tool { return &grepTool{} }

func (t *grepTool) Name() string { return GrepToolName }

func (t *grepTool) Description() string { return firstLine(grepDescription) }

func (t *grepTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        GrepToolName,
		Description: t.Description(),
		Parameters: map[string]any{
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
					"description": "File pattern to include in the search",
				},
				"literal_text": map[string]any{
					"type":        "boolean",
					"description": "If true, the pattern will be treated as literal text",
				},
			},
			"required": []string{"pattern"},
		},
	}
}

func (t *grepTool) Execute(ctx context.Context, tctx tools.ToolContext, args json.RawMessage) (tools.ToolResult, error) {
	var params GrepParams
	if err := json.Unmarshal(args, &params); err != nil {
		return tools.ErrorResult(err)
	}
	if params.Pattern == "" {
		return tools.ToolResult{Error: errors.New("pattern is required")}, nil
	}

	searchPattern := params.Pattern
	if params.LiteralText {
		searchPattern = regexp.QuoteMeta(params.Pattern)
	}

	searchPath := params.Path
	if searchPath == "" && tctx != nil {
		searchPath = tctx.WorkingDir()
	}
	if searchPath == "" {
		searchPath = "."
	}

	searchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	matches, truncated, err := SearchFiles(searchCtx, searchPattern, searchPath, params.Include, 100)
	if err != nil {
		return tools.ErrorResult(fmt.Errorf("error searching files: %w", err))
	}

	var output strings.Builder
	if len(matches) == 0 {
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
			lineText := match.LineText
			if len(lineText) > maxGrepContentWidth {
				lineText = lineText[:maxGrepContentWidth] + "..."
			}
			if match.CharNum > 0 {
				fmt.Fprintf(&output, "  Line %d, Char %d: %s\n", match.LineNum, match.CharNum, lineText)
			} else {
				fmt.Fprintf(&output, "  Line %d: %s\n", match.LineNum, lineText)
			}
		}

		if truncated {
			output.WriteString("\n(Results are truncated. Consider using a more specific path or pattern.)")
		}
	}

	return tools.ToolResult{
		Content: output.String(),
		Metadata: map[string]any{
			"number_of_matches": len(matches),
			"truncated":         truncated,
		},
	}, nil
}

func SearchFiles(ctx context.Context, pattern, rootPath, include string, limit int) ([]GrepMatch, bool, error) {
	matches, err := searchWithRipgrep(ctx, pattern, rootPath, include)
	if err != nil {
		matches, err = searchFilesWithRegex(pattern, rootPath, include)
		if err != nil {
			return nil, false, err
		}
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].ModTime.After(matches[j].ModTime)
	})

	truncated := len(matches) > limit
	if truncated {
		matches = matches[:limit]
	}

	return matches, truncated, nil
}

func searchWithRipgrep(ctx context.Context, pattern, path, include string) ([]GrepMatch, error) {
	cmd := searchcommon.GetRgSearchCmd(ctx, pattern, path, include)
	if cmd == nil {
		return nil, errors.New("ripgrep not found in $PATH")
	}

	for _, ignoreFile := range []string{".gitignore", ".mocodeignore"} {
		ignorePath := filepath.Join(path, ignoreFile)
		if _, err := os.Stat(ignorePath); err == nil {
			cmd.Args = append(cmd.Args, "--ignore-file", ignorePath)
		}
	}

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return []GrepMatch{}, nil
		}
		return nil, err
	}

	var matches []GrepMatch
	for line := range bytes.SplitSeq(bytes.TrimSpace(output), []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var match ripgrepMatch
		if err := json.Unmarshal(line, &match); err != nil {
			continue
		}
		if match.Type != "match" {
			continue
		}
		for _, m := range match.Data.Submatches {
			fi, err := os.Stat(match.Data.Path.Text)
			if err != nil {
				continue
			}
			matches = append(matches, GrepMatch{
				Path:     match.Data.Path.Text,
				ModTime:  fi.ModTime(),
				LineNum:  match.Data.LineNumber,
				CharNum:  m.Start + 1,
				LineText: strings.TrimSpace(match.Data.Lines.Text),
			})
			break
		}
	}
	return matches, nil
}

type ripgrepMatch struct {
	Type string `json:"type"`
	Data struct {
		Path struct {
			Text string `json:"text"`
		} `json:"path"`
		Lines struct {
			Text string `json:"text"`
		} `json:"lines"`
		LineNumber int `json:"line_number"`
		Submatches []struct {
			Start int `json:"start"`
		} `json:"submatches"`
	} `json:"data"`
}

func searchFilesWithRegex(pattern, rootPath, include string) ([]GrepMatch, error) {
	matches := []GrepMatch{}

	regex, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex pattern: %w", err)
	}

	var includePattern *regexp.Regexp
	if include != "" {
		regexPattern := globToRegex(include)
		includePattern, err = regexp.Compile(regexPattern)
		if err != nil {
			return nil, fmt.Errorf("invalid include pattern: %w", err)
		}
	}

	walker := fsext.NewFastGlobWalker(rootPath)

	err = filepath.Walk(rootPath, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return skipWalkError(walkErr)
		}

		if info.IsDir() {
			if walker.ShouldSkip(path) {
				return filepath.SkipDir
			}
			return nil
		}

		if walker.ShouldSkip(path) {
			return nil
		}

		base := filepath.Base(path)
		if base != "." && strings.HasPrefix(base, ".") {
			return nil
		}

		if includePattern != nil && !includePattern.MatchString(path) {
			return nil
		}

		match, lineNum, charNum, lineText, readErr := fileContainsPattern(path, regex)
		if readErr != nil {
			return skipWalkError(readErr)
		}

		if match {
			matches = append(matches, GrepMatch{
				Path:     path,
				ModTime:  info.ModTime(),
				LineNum:  lineNum,
				CharNum:  charNum,
				LineText: lineText,
			})

			if len(matches) >= 200 {
				return filepath.SkipAll
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return matches, nil
}

func skipWalkError(error) error { return nil }

func fileContainsPattern(filePath string, pattern *regexp.Regexp) (bool, int, int, string, error) {
	if !isTextFile(filePath) {
		return false, 0, 0, "", nil
	}

	file, err := os.Open(filePath)
	if err != nil {
		return false, 0, 0, "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if loc := pattern.FindStringIndex(line); loc != nil {
			return true, lineNum, loc[0] + 1, line, nil
		}
	}

	return false, 0, 0, "", scanner.Err()
}

func isTextFile(filePath string) bool {
	file, err := os.Open(filePath)
	if err != nil {
		return false
	}
	defer file.Close()

	buffer := make([]byte, 512)
	n, err := file.Read(buffer)
	if err != nil && err != io.EOF {
		return false
	}

	contentType := http.DetectContentType(buffer[:n])
	return strings.HasPrefix(contentType, "text/") ||
		contentType == "application/json" ||
		contentType == "application/xml" ||
		contentType == "application/javascript" ||
		contentType == "application/x-sh"
}

func globToRegex(glob string) string {
	regexPattern := strings.ReplaceAll(glob, ".", "\\.")
	regexPattern = strings.ReplaceAll(regexPattern, "*", ".*")
	regexPattern = strings.ReplaceAll(regexPattern, "?", ".")
	regexPattern = globBraceRegex.ReplaceAllStringFunc(regexPattern, func(match string) string {
		inner := match[1 : len(match)-1]
		return "(" + strings.ReplaceAll(inner, ",", "|") + ")"
	})
	return regexPattern
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

func init() { tools.Register(GrepToolName, New()) }
