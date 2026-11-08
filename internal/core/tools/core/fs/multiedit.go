package fs

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/core/tools/contract"

	"github.com/nextsko/mocode-agent/internal/core/permission"
	"github.com/nextsko/mocode-agent/internal/core/tools/internalx/lsp"
	"github.com/nextsko/mocode-agent/internal/domain/filetracker"
	"github.com/nextsko/mocode-agent/internal/domain/history"
	"github.com/nextsko/mocode-agent/internal/util/diff"
	"github.com/nextsko/mocode-agent/internal/util/ext"
	"github.com/nextsko/mocode-agent/internal/util/fsext"
)

type MultiEditOperation struct {
	OldString  string `json:"old_string" description:"The text to replace"`
	NewString  string `json:"new_string" description:"The text to replace it with"`
	ReplaceAll bool   `json:"replace_all,omitempty" description:"Replace all occurrences of old_string (default false)."`
}

type MultiEditParams struct {
	FilePath string               `json:"file_path" description:"The absolute path to the file to modify"`
	Edits    []MultiEditOperation `json:"edits" description:"Array of edit operations to perform sequentially on the file"`
}

type MultiEditPermissionsParams struct {
	FilePath   string `json:"file_path"`
	OldContent string `json:"old_content,omitempty"`
	NewContent string `json:"new_content,omitempty"`
}

type FailedEdit struct {
	Index int                `json:"index"`
	Error string             `json:"error"`
	Edit  MultiEditOperation `json:"edit"`
}

type MultiEditResponseMetadata struct {
	Additions    int          `json:"additions"`
	Removals     int          `json:"removals"`
	OldContent   string       `json:"old_content,omitempty"`
	NewContent   string       `json:"new_content,omitempty"`
	EditsApplied int          `json:"edits_applied"`
	EditsFailed  []FailedEdit `json:"edits_failed,omitempty"`
}

const MultiEditToolName = "multiedit"

//go:embed multiedit.md
var multieditDescription []byte

type multiEditContext struct {
	ctx         context.Context
	permissions permission.Service
	files       history.Service
	filetracker filetracker.Service
	workingDir  string
}

// NewMultiEditPortTool is the framework-agnostic form of the multiedit tool.
func NewMultiEditPortTool(
	lspManager *lsp.Manager,
	permissions permission.Service,
	files history.Service,
	filetracker filetracker.Service,
	workingDir string,
) contract.Tool {
	return contract.NewFunc(
		MultiEditToolName,
		toolutil.FirstLineDescription(multieditDescription),
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"file_path": map[string]any{
					"type":        "string",
					"description": "The absolute path to the file to modify",
				},
				"edits": map[string]any{
					"type":        "array",
					"description": "Array of edit operations to perform sequentially on the file",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"old_string": map[string]any{
								"type":        "string",
								"description": "The text to replace",
							},
							"new_string": map[string]any{
								"type":        "string",
								"description": "The text to replace it with",
							},
							"replace_all": map[string]any{
								"type":        "boolean",
								"description": "Replace all occurrences of old_string (default false).",
							},
						},
						"required": []string{"old_string", "new_string"},
					},
				},
			},
			"required": []string{"file_path", "edits"},
		},
		func(ctx context.Context, _ contract.ToolContext, args json.RawMessage) (contract.ToolResult, error) {
			var params MultiEditParams
			if err := json.Unmarshal(args, &params); err != nil {
				return contract.ToolResult{}, fmt.Errorf("invalid parameters: %w", err)
			}
			callID, _ := toolutil.ToolCallIDFromCtx(ctx)
			return runMultiEdit(ctx, lspManager, permissions, files, filetracker, workingDir, params, callID)
		},
	)
}

// runMultiEdit applies the edit batch and appends LSP diagnostics.
func runMultiEdit(
	ctx context.Context,
	lspManager *lsp.Manager,
	permissions permission.Service,
	files history.Service,
	filetracker filetracker.Service,
	workingDir string,
	params MultiEditParams,
	callID string,
) (contract.ToolResult, error) {
	if params.FilePath == "" {
		return contract.Error("file_path is required"), nil
	}

	if len(params.Edits) == 0 {
		return contract.Error("at least one edit operation is required"), nil
	}

	params.FilePath = ext.SmartJoin(workingDir, params.FilePath)

	// Validate all edits before applying any
	if err := validateEdits(params.Edits); err != nil {
		return contract.Error(err.Error()), nil
	}

	var response contract.ToolResult
	var err error

	editCtx := multiEditContext{ctx, permissions, files, filetracker, workingDir}
	// Handle file creation case (first edit has empty old_string)
	if len(params.Edits) > 0 && params.Edits[0].OldString == "" {
		response, err = processMultiEditWithCreation(editCtx, params, callID)
	} else {
		response, err = processMultiEditExistingFile(editCtx, params, callID)
	}

	if err != nil {
		return response, err
	}

	// A tool-level failure (bad edit) returns early without LSP diagnostics
	// processing.
	if response.Error == nil {
		// Notify LSP clients about the change
		lsp.NotifyLSPs(ctx, lspManager, params.FilePath)

		// Wait for LSP diagnostics and add them to the response
		text := fmt.Sprintf("<result>\n%s\n</result>\n", response.Content)
		text += lsp.GetDiagnostics(params.FilePath, lspManager)
		response.Content = text
	}
	return response, nil
}

func validateEdits(edits []MultiEditOperation) error {
	for i, edit := range edits {
		// Only the first edit can have empty old_string (for file creation)
		if i > 0 && edit.OldString == "" {
			return fmt.Errorf("edit %d: only the first edit can have empty old_string (for file creation)", i+1)
		}
	}
	return nil
}

func processMultiEditWithCreation(edit multiEditContext, params MultiEditParams, callID string) (contract.ToolResult, error) {
	// First edit creates the file
	firstEdit := params.Edits[0]
	if firstEdit.OldString != "" {
		return contract.Error("first edit must have empty old_string for file creation"), nil
	}

	// Check if file already exists
	if _, err := os.Stat(params.FilePath); err == nil {
		return contract.Error(fmt.Sprintf("file already exists: %s", params.FilePath)), nil
	} else if !os.IsNotExist(err) {
		return contract.ToolResult{}, fmt.Errorf("failed to access file: %w", err)
	}

	// Create parent directories
	dir := filepath.Dir(params.FilePath)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return contract.ToolResult{}, fmt.Errorf("failed to create parent directories: %w", err)
	}

	// Start with the content from the first edit
	currentContent := firstEdit.NewString

	// Apply remaining edits to the content, tracking failures
	var failedEdits []FailedEdit
	for i := 1; i < len(params.Edits); i++ {
		edit := params.Edits[i]
		newContent, err := applyEditToContent(currentContent, edit)
		if err != nil {
			failedEdits = append(failedEdits, FailedEdit{
				Index: i + 1,
				Error: err.Error(),
				Edit:  edit,
			})
			continue
		}
		currentContent = newContent
	}

	// Get session and message IDs
	sessionID := toolutil.GetSessionFromContext(edit.ctx)
	if sessionID == "" {
		return contract.ToolResult{}, fmt.Errorf("session ID is required for creating a new file")
	}

	// Check permissions
	_, additions, removals := diff.GenerateDiff("", currentContent, strings.TrimPrefix(params.FilePath, edit.workingDir))

	editsApplied := len(params.Edits) - len(failedEdits)
	var description string
	if len(failedEdits) > 0 {
		description = fmt.Sprintf("Create file %s with %d of %d edits (%d failed)", params.FilePath, editsApplied, len(params.Edits), len(failedEdits))
	} else {
		description = fmt.Sprintf("Create file %s with %d edits", params.FilePath, editsApplied)
	}
	p, err := edit.permissions.Request(edit.ctx, permission.CreatePermissionRequest{
		SessionID:   sessionID,
		Path:        fsext.PathOrPrefix(params.FilePath, edit.workingDir),
		ToolCallID:  callID,
		ToolName:    MultiEditToolName,
		Action:      "write",
		Description: description,
		Params: MultiEditPermissionsParams{
			FilePath:   params.FilePath,
			OldContent: "",
			NewContent: currentContent,
		},
	})
	if err != nil {
		return contract.ToolResult{}, err
	}
	if !p {
		return permissionDeniedResult(), nil
	}

	// Write the file
	err = os.WriteFile(params.FilePath, []byte(currentContent), 0o600)
	if err != nil {
		return contract.ToolResult{}, fmt.Errorf("failed to write file: %w", err)
	}

	// Update file history
	_, err = edit.files.Create(edit.ctx, sessionID, params.FilePath, "")
	if err != nil {
		return contract.ToolResult{}, fmt.Errorf("error creating file history: %w", err)
	}

	_, err = edit.files.CreateVersion(edit.ctx, sessionID, params.FilePath, currentContent)
	if err != nil {
		slog.Error("Error creating file history version", "error", err)
	}

	edit.filetracker.RecordRead(edit.ctx, sessionID, params.FilePath)

	var message string
	if len(failedEdits) > 0 {
		message = fmt.Sprintf("File created with %d of %d edits: %s (%d edit(s) failed)", editsApplied, len(params.Edits), params.FilePath, len(failedEdits))
	} else {
		message = fmt.Sprintf("File created with %d edits: %s", len(params.Edits), params.FilePath)
	}

	return contract.WithMetadata(contract.Text(message), MultiEditResponseMetadata{
		OldContent:   "",
		NewContent:   currentContent,
		Additions:    additions,
		Removals:     removals,
		EditsApplied: editsApplied,
		EditsFailed:  failedEdits,
	}), nil
}

func processMultiEditExistingFile(edit multiEditContext, params MultiEditParams, callID string) (contract.ToolResult, error) {
	// Validate file exists and is readable
	fileInfo, err := os.Stat(params.FilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return contract.Error(fmt.Sprintf("file not found: %s", params.FilePath)), nil
		}
		return contract.ToolResult{}, fmt.Errorf("failed to access file: %w", err)
	}

	if fileInfo.IsDir() {
		return contract.Error(fmt.Sprintf("path is a directory, not a file: %s", params.FilePath)), nil
	}

	sessionID := toolutil.GetSessionFromContext(edit.ctx)
	if sessionID == "" {
		return contract.ToolResult{}, fmt.Errorf("session ID is required for editing file")
	}

	// Check if file was read before editing
	lastRead := edit.filetracker.LastReadTime(edit.ctx, sessionID, params.FilePath)
	if lastRead.IsZero() {
		return contract.Error("you must read the file before editing it. Use the View tool first"), nil
	}

	// Check if file was modified since last read.
	modTime := fileInfo.ModTime().Truncate(time.Second)
	if modTime.After(lastRead) {
		return contract.Error(
			fmt.Sprintf("file %s has been modified since it was last read (mod time: %s, last read: %s)",
				params.FilePath, modTime.Format(time.RFC3339), lastRead.Format(time.RFC3339),
			)), nil
	}

	// Read current file content
	content, err := os.ReadFile(params.FilePath)
	if err != nil {
		return contract.ToolResult{}, fmt.Errorf("failed to read file: %w", err)
	}

	oldContent, isCrlf := fsext.ToUnixLineEndings(string(content))
	currentContent := oldContent

	// Apply all edits sequentially, tracking failures
	var failedEdits []FailedEdit
	for i, edit := range params.Edits {
		newContent, err := applyEditToContent(currentContent, edit)
		if err != nil {
			failedEdits = append(failedEdits, FailedEdit{
				Index: i + 1,
				Error: err.Error(),
				Edit:  edit,
			})
			continue
		}
		currentContent = newContent
	}

	// Check if content actually changed
	if oldContent == currentContent {
		// If we have failed edits, report them
		if len(failedEdits) > 0 {
			return contract.WithMetadata(
				contract.Error(fmt.Sprintf("no changes made - all %d edit(s) failed", len(failedEdits))),
				MultiEditResponseMetadata{
					EditsApplied: 0,
					EditsFailed:  failedEdits,
				},
			), nil
		}
		return contract.Error("no changes made - all edits resulted in identical content"), nil
	}

	// Generate diff and check permissions
	_, additions, removals := diff.GenerateDiff(oldContent, currentContent, strings.TrimPrefix(params.FilePath, edit.workingDir))

	editsApplied := len(params.Edits) - len(failedEdits)
	var description string
	if len(failedEdits) > 0 {
		description = fmt.Sprintf("Apply %d of %d edits to file %s (%d failed)", editsApplied, len(params.Edits), params.FilePath, len(failedEdits))
	} else {
		description = fmt.Sprintf("Apply %d edits to file %s", editsApplied, params.FilePath)
	}
	p, err := edit.permissions.Request(edit.ctx, permission.CreatePermissionRequest{
		SessionID:   sessionID,
		Path:        fsext.PathOrPrefix(params.FilePath, edit.workingDir),
		ToolCallID:  callID,
		ToolName:    MultiEditToolName,
		Action:      "write",
		Description: description,
		Params: MultiEditPermissionsParams{
			FilePath:   params.FilePath,
			OldContent: oldContent,
			NewContent: currentContent,
		},
	})
	if err != nil {
		return contract.ToolResult{}, err
	}
	if !p {
		return permissionDeniedResult(), nil
	}

	if isCrlf {
		currentContent, _ = fsext.ToWindowsLineEndings(currentContent)
	}

	// Write the updated content
	err = os.WriteFile(params.FilePath, []byte(currentContent), 0o600)
	if err != nil {
		return contract.ToolResult{}, fmt.Errorf("failed to write file: %w", err)
	}

	// Update file history
	file, err := edit.files.GetByPathAndSession(edit.ctx, params.FilePath, sessionID)
	if err != nil {
		_, err = edit.files.Create(edit.ctx, sessionID, params.FilePath, oldContent)
		if err != nil {
			return contract.ToolResult{}, fmt.Errorf("error creating file history: %w", err)
		}
	}
	if file.Content != oldContent {
		// User manually changed the content, store an intermediate version
		_, err = edit.files.CreateVersion(edit.ctx, sessionID, params.FilePath, oldContent)
		if err != nil {
			slog.Error("Error creating file history version", "error", err)
		}
	}

	// Store the new version
	_, err = edit.files.CreateVersion(edit.ctx, sessionID, params.FilePath, currentContent)
	if err != nil {
		slog.Error("Error creating file history version", "error", err)
	}

	edit.filetracker.RecordRead(edit.ctx, sessionID, params.FilePath)

	var message string
	if len(failedEdits) > 0 {
		message = fmt.Sprintf("Applied %d of %d edits to file: %s (%d edit(s) failed)", editsApplied, len(params.Edits), params.FilePath, len(failedEdits))
	} else {
		message = fmt.Sprintf("Applied %d edits to file: %s", len(params.Edits), params.FilePath)
	}

	return contract.WithMetadata(contract.Text(message), MultiEditResponseMetadata{
		OldContent:   oldContent,
		NewContent:   currentContent,
		Additions:    additions,
		Removals:     removals,
		EditsApplied: editsApplied,
		EditsFailed:  failedEdits,
	}), nil
}

func applyEditToContent(content string, edit MultiEditOperation) (string, error) {
	if edit.OldString == "" && edit.NewString == "" {
		return content, nil
	}

	if edit.OldString == "" {
		return "", fmt.Errorf("old_string cannot be empty for content replacement")
	}

	var newContent string
	var replacementCount int

	if edit.ReplaceAll {
		newContent = strings.ReplaceAll(content, edit.OldString, edit.NewString)
		replacementCount = strings.Count(content, edit.OldString)
		if replacementCount == 0 {
			return "", fmt.Errorf("old_string not found in content. Make sure it matches exactly, including whitespace and line breaks")
		}
	} else {
		index := strings.Index(content, edit.OldString)
		if index == -1 {
			return "", fmt.Errorf("old_string not found in content. Make sure it matches exactly, including whitespace and line breaks")
		}

		lastIndex := strings.LastIndex(content, edit.OldString)
		if index != lastIndex {
			return "", fmt.Errorf("old_string appears multiple times in the content. Please provide more context to ensure a unique match, or set replace_all to true")
		}

		newContent = content[:index] + edit.NewString + content[index+len(edit.OldString):]
		replacementCount = 1
	}

	return newContent, nil
}
