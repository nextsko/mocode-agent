package fs

import (
	"bufio"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"

	"charm.land/fantasy"

	"github.com/nextsko/mocode-agent/internal/core/permission"
	"github.com/nextsko/mocode-agent/internal/core/skills"
	"github.com/nextsko/mocode-agent/internal/core/tools/internalx/lsp"
	"github.com/nextsko/mocode-agent/internal/core/uri"
	"github.com/nextsko/mocode-agent/internal/domain/filetracker"
	"github.com/nextsko/mocode-agent/internal/util/ext"
)

//go:embed view.md
var viewDescription []byte

type ViewParams struct {
	FilePath string `json:"file_path" description:"The path to the file to read"`
	Offset   int    `json:"offset,omitempty" description:"The line number to start reading from (0-based)"`
	Limit    int    `json:"limit,omitempty" description:"The number of lines to read (defaults to 2000)"`
}

type ViewPermissionsParams struct {
	FilePath string `json:"file_path"`
	Offset   int    `json:"offset"`
	Limit    int    `json:"limit"`
}

type ViewResourceType string

const (
	ViewResourceUnset ViewResourceType = ""
	ViewResourceSkill ViewResourceType = "skill"
)

type ViewResponseMetadata struct {
	FilePath            string           `json:"file_path"`
	Content             string           `json:"content"`
	ResourceType        ViewResourceType `json:"resource_type,omitempty"`
	ResourceName        string           `json:"resource_name,omitempty"`
	ResourceDescription string           `json:"resource_description,omitempty"`
}

const (
	ViewToolName     = "view"
	MaxViewSize      = 200 * 1024 // 200KB
	DefaultReadLimit = 2000
	MaxLineLength    = 2000
)

func NewViewTool(
	lspManager *lsp.Manager,
	permissions permission.Service,
	filetracker filetracker.Service,
	skillTracker *skills.Tracker,
	uriRegistry *uri.Registry,
	workingDir string,
	skillsPaths ...string,
) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		ViewToolName,
		toolutil.FirstLineDescription(viewDescription),
		func(ctx context.Context, params ViewParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.FilePath == "" {
				return fantasy.NewTextErrorResponse("file_path is required"), nil
			}

			// Internal resources (mocode://...) resolve through the protocol
			// registry rather than the filesystem. Everything the scheme defines
			// arrives here: embedded skills, the docs and experience namespaces,
			// GitHub files, and the live browser session.
			if uri.IsURI(params.FilePath) {
				return readURIResource(ctx, params, uriRegistry, skillTracker)
			}

			// Handle relative paths
			filePath := ext.SmartJoin(workingDir, params.FilePath)

			// Check if file is outside working directory and request permission if needed
			absWorkingDir, err := filepath.Abs(workingDir)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("error resolving working directory: %w", err)
			}

			absFilePath, err := filepath.Abs(filePath)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("error resolving file path: %w", err)
			}

			relPath, err := filepath.Rel(absWorkingDir, absFilePath)
			isOutsideWorkDir := err != nil || strings.HasPrefix(relPath, "..")
			isSkillFile := isInSkillsPath(absFilePath, skillsPaths)

			sessionID := toolutil.GetSessionFromContext(ctx)
			if sessionID == "" {
				return fantasy.ToolResponse{}, fmt.Errorf("session ID is required for accessing files outside working directory")
			}

			// Request permission for files outside working directory, unless it's a skill file.
			if isOutsideWorkDir && !isSkillFile {
				granted, permReqErr := permissions.Request(ctx,
					permission.CreatePermissionRequest{
						SessionID:   sessionID,
						Path:        absFilePath,
						ToolCallID:  call.ID,
						ToolName:    ViewToolName,
						Action:      "read",
						Description: fmt.Sprintf("Read file outside working directory: %s", absFilePath),
						Params:      ViewPermissionsParams(params),
					},
				)
				if permReqErr != nil {
					return fantasy.ToolResponse{}, permReqErr
				}
				if !granted {
					return toolutil.NewPermissionDeniedResponse(), nil
				}
			}

			// Check if file exists
			fileInfo, err := os.Stat(filePath)
			if err != nil {
				if os.IsNotExist(err) {
					// Try to offer suggestions for similarly named files
					dir := filepath.Dir(filePath)
					base := filepath.Base(filePath)

					dirEntries, dirErr := os.ReadDir(dir)
					if dirErr == nil {
						var suggestions []string
						for _, entry := range dirEntries {
							if strings.Contains(strings.ToLower(entry.Name()), strings.ToLower(base)) ||
								strings.Contains(strings.ToLower(base), strings.ToLower(entry.Name())) {
								suggestions = append(suggestions, filepath.Join(dir, entry.Name()))
								if len(suggestions) >= 3 {
									break
								}
							}
						}

						if len(suggestions) > 0 {
							return fantasy.NewTextErrorResponse(fmt.Sprintf("File not found: %s\n\nDid you mean one of these?\n%s",
								filePath, strings.Join(suggestions, "\n"))), nil
						}
					}

					return fantasy.NewTextErrorResponse(fmt.Sprintf("File not found: %s", filePath)), nil
				}
				return fantasy.ToolResponse{}, fmt.Errorf("error accessing file: %w", err)
			}

			// Check if it's a directory
			if fileInfo.IsDir() {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("Path is a directory, not a file: %s", filePath)), nil
			}

			// Based on the specifications we should not limit the skills read.
			if !isSkillFile && fileInfo.Size() > MaxViewSize {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("File is too large (%d bytes). Maximum size is %d bytes",
					fileInfo.Size(), MaxViewSize)), nil
			}

			// Set default limit if not provided (no limit for SKILL.md files)
			if params.Limit <= 0 {
				if isSkillFile {
					params.Limit = 1000000 // Effectively no limit for skill files
				} else {
					params.Limit = DefaultReadLimit
				}
			}

			isSupportedImage, mimeType := getImageMimeType(filePath)
			if isSupportedImage {
				if !toolutil.GetSupportsImagesFromContext(ctx) {
					modelName := toolutil.GetModelNameFromContext(ctx)
					return fantasy.NewTextErrorResponse(fmt.Sprintf("This model (%s) does not support image data.", modelName)), nil
				}

				imageData, readErr := os.ReadFile(filePath)
				if readErr != nil {
					return fantasy.ToolResponse{}, fmt.Errorf("error reading image file: %w", readErr)
				}

				return fantasy.NewImageResponse(imageData, mimeType), nil
			}

			// Read the file content
			content, hasMore, err := readTextFile(filePath, params.Offset, params.Limit)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("error reading file: %w", err)
			}
			if !utf8.ValidString(content) {
				return fantasy.NewTextErrorResponse("File content is not valid UTF-8"), nil
			}

			lsp.OpenInLSPs(ctx, lspManager, filePath)
			lsp.WaitForLSPDiagnostics(ctx, lspManager, filePath, 300*time.Millisecond)
			output := "<file>\n"
			output += addLineNumbers(content, params.Offset+1)

			if hasMore {
				output += fmt.Sprintf("\n\n(File has more lines. Use 'offset' parameter to read beyond line %d)",
					params.Offset+len(strings.Split(content, "\n")))
			}
			output += "\n</file>\n"
			output += lsp.GetDiagnostics(filePath, lspManager)
			filetracker.RecordRead(ctx, sessionID, filePath)

			meta := ViewResponseMetadata{
				FilePath: filePath,
				Content:  content,
			}
			if isSkillFile {
				if skill, err := skills.Parse(filePath); err == nil {
					meta.ResourceType = ViewResourceSkill
					meta.ResourceName = skill.Name
					meta.ResourceDescription = skill.Description
					skillTracker.MarkLoaded(skill.Name)
				}
			}

			return fantasy.WithResponseMetadata(
				fantasy.NewTextResponse(output),
				meta,
			), nil
		})
}

func addLineNumbers(content string, startLine int) string {
	if content == "" {
		return ""
	}

	lines := strings.Split(content, "\n")

	var result []string
	for i, line := range lines {
		line = strings.TrimSuffix(line, "\r")

		lineNum := i + startLine
		numStr := fmt.Sprintf("%d", lineNum)

		if len(numStr) >= 6 {
			result = append(result, fmt.Sprintf("%s|%s", numStr, line))
		} else {
			paddedNum := fmt.Sprintf("%6s", numStr)
			result = append(result, fmt.Sprintf("%s|%s", paddedNum, line))
		}
	}

	return strings.Join(result, "\n")
}

func readTextFile(filePath string, offset, limit int) (string, bool, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", false, err
	}
	defer file.Close()

	scanner := NewLineScanner(file)
	if offset > 0 {
		skipped := 0
		for skipped < offset && scanner.Scan() {
			skipped++
		}
		if err = scanner.Err(); err != nil {
			return "", false, err
		}
	}

	// Pre-allocate slice with expected capacity.
	lines := make([]string, 0, limit)

	for len(lines) < limit && scanner.Scan() {
		lineText := scanner.Text()
		if len(lineText) > MaxLineLength {
			lineText = lineText[:MaxLineLength] + "..."
		}
		lines = append(lines, lineText)
	}

	// Peek one more line only when we filled the limit.
	hasMore := len(lines) == limit && scanner.Scan()

	if err := scanner.Err(); err != nil {
		return "", false, err
	}

	return strings.Join(lines, "\n"), hasMore, nil
}

func getImageMimeType(filePath string) (bool, string) {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".jpg", ".jpeg":
		return true, "image/jpeg"
	case ".png":
		return true, "image/png"
	case ".gif":
		return true, "image/gif"
	case ".webp":
		return true, "image/webp"
	default:
		return false, ""
	}
}

type LineScanner struct {
	scanner *bufio.Scanner
}

func NewLineScanner(r io.Reader) *LineScanner {
	scanner := bufio.NewScanner(r)
	// Increase buffer size to handle large lines (e.g., minified JSON, HTML)
	// Default is 64KB, set to 1MB
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	return &LineScanner{
		scanner: scanner,
	}
}

func (s *LineScanner) Scan() bool {
	return s.scanner.Scan()
}

func (s *LineScanner) Text() string {
	return s.scanner.Text()
}

func (s *LineScanner) Err() error {
	return s.scanner.Err()
}

// isInSkillsPath reports whether filePath lies inside a configured skills
// directory. Such files are readable without a permission prompt and without
// size limits, because the agent is expected to consult its own skills.
//
// The containment test lives in the skills package so that it is the same test
// the rest of the program uses, rather than a second implementation that can
// drift from it.
func isInSkillsPath(filePath string, skillsPaths []string) bool {
	return skills.SkillsDirIn(filePath, skillsPaths)
}

// readURIResource resolves a mocode:// reference through the protocol registry.
//
// This is the single entry point for every non-filesystem resource. The mapping
// used to be a hardcoded prefix check plus a direct read of the embedded skills
// filesystem; a registry means one code path, one error message vocabulary, and
// new namespaces that need no change here.
func readURIResource(
	ctx context.Context,
	params ViewParams,
	registry *uri.Registry,
	skillTracker *skills.Tracker,
) (fantasy.ToolResponse, error) {
	if registry == nil {
		return fantasy.NewTextErrorResponse(
			"internal mocode:// resources are not available in this session"), nil
	}

	content, u, err := registry.Resolve(ctx, params.FilePath)
	if err != nil {
		// A bad reference is the model's mistake to correct, so it comes back
		// as guidance it can act on rather than as a tool failure.
		return fantasy.NewTextErrorResponse(uriGuidance(err, registry)), nil
	}

	meta := ViewResponseMetadata{
		FilePath:            params.FilePath,
		ResourceType:        resourceTypeFor(u.Authority),
		ResourceName:        content.Title,
		ResourceDescription: content.Description,
	}

	switch content.Kind {
	case uri.KindListing:
		text := uri.RenderListing(u, content)
		meta.Content = text
		return fantasy.WithResponseMetadata(fantasy.NewTextResponse(text), meta), nil

	case uri.KindImage:
		return imageURIResponse(params, content, meta)

	default:
		return textURIResponse(params, u, content, meta, skillTracker)
	}
}

// textURIResponse applies the same offset/limit/line-number treatment to remote
// text as to a local file, so reading a GitHub file and reading a local one look
// identical to the model.
func textURIResponse(
	params ViewParams,
	u uri.URI,
	content *uri.Content,
	meta ViewResponseMetadata,
	skillTracker *skills.Tracker,
) (fantasy.ToolResponse, error) {
	// Skills are the agent's own instructions, so they are read whole; anything
	// else is windowed like a file.
	limit := params.Limit
	if limit <= 0 {
		limit = DefaultReadLimit
		if u.Authority == uri.AuthoritySkills {
			limit = 1000000
		}
	}

	lines := strings.Split(content.Text, "\n")
	offset := min(max(0, params.Offset), len(lines))
	lines = lines[offset:]

	hasMore := len(lines) > limit
	if hasMore {
		lines = lines[:limit]
	}

	var output strings.Builder
	output.WriteString("<file>\n")
	output.WriteString(addLineNumbers(strings.Join(lines, "\n"), offset+1))
	if hasMore {
		fmt.Fprintf(&output, "\n\n(File has more lines. Use 'offset' parameter to read beyond line %d)",
			offset+len(lines))
	}
	output.WriteString("\n</file>\n")

	meta.Content = strings.Join(lines, "\n")

	// A skill read is how the model "loads" a skill; recording it keeps the
	// loaded/unloaded state the prompt uses accurate.
	if u.Authority == uri.AuthoritySkills && skillTracker != nil && strings.HasSuffix(u.Path, skills.SkillFileName) {
		if skill, perr := skills.ParseContent([]byte(content.Text)); perr == nil {
			meta.ResourceType = ViewResourceSkill
			meta.ResourceName = skill.Name
			meta.ResourceDescription = skill.Description
			skillTracker.MarkLoaded(skill.Name)
		}
	}

	return fantasy.WithResponseMetadata(fantasy.NewTextResponse(output.String()), meta), nil
}

// imageURIResponse returns binary image content the model can look at.
func imageURIResponse(params ViewParams, content *uri.Content, meta ViewResponseMetadata) (fantasy.ToolResponse, error) {
	meta.Content = fmt.Sprintf("<image %s, %d bytes>", content.MIME, len(content.Data))
	return fantasy.WithResponseMetadata(
		fantasy.NewImageResponse(content.Data, content.MIME),
		meta,
	), nil
}

// resourceTypeFor maps an authority onto the metadata type the UI shows,
// preserving the historical "skill" label the skills namespace already used.
func resourceTypeFor(authority string) ViewResourceType {
	if authority == uri.AuthoritySkills {
		return ViewResourceSkill
	}
	return ViewResourceType(authority)
}

// uriGuidance turns a resolution failure into something the model can act on:
// the namespace map for an unknown authority, or a pointer at a listing for a
// missing path.
func uriGuidance(err error, registry *uri.Registry) string {
	msg := fmt.Sprintf("Failed to resolve internal resource: %v", err)

	if errors.Is(err, uri.ErrUnknownAuthority) || errors.Is(err, uri.ErrNotURI) {
		return msg + "\n\n" + registry.Describe()
	}
	return msg + "\n\nAddress a namespace without a path to list what it contains, e.g. mocode://docs/"
}
