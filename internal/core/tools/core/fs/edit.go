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

	"github.com/nextsko/mocode-agent/internal/domain/filetracker"
	"github.com/nextsko/mocode-agent/internal/domain/history"
	"github.com/nextsko/mocode-agent/internal/util/diff"
	"github.com/nextsko/mocode-agent/internal/util/ext"
	"github.com/nextsko/mocode-agent/internal/util/fsext"

	"github.com/nextsko/mocode-agent/internal/core/permission"
	"github.com/nextsko/mocode-agent/internal/core/tools/internalx/lsp"
)

type EditParams struct {
	FilePath   string `json:"file_path" description:"The absolute path to the file to modify"`
	OldString  string `json:"old_string" description:"The text to replace"`
	NewString  string `json:"new_string" description:"The text to replace it with"`
	ReplaceAll bool   `json:"replace_all,omitempty" description:"Replace all occurrences of old_string (default false)"`
}

type EditPermissionsParams struct {
	FilePath   string `json:"file_path"`
	OldContent string `json:"old_content,omitempty"`
	NewContent string `json:"new_content,omitempty"`
}

type EditResponseMetadata struct {
	Additions  int    `json:"additions"`
	Removals   int    `json:"removals"`
	OldContent string `json:"old_content,omitempty"`
	NewContent string `json:"new_content,omitempty"`
}

const EditToolName = "edit"

var (
	oldStringNotFoundErr        = contract.Error("old_string not found in file. Make sure it matches exactly, including whitespace and line breaks.")
	oldStringMultipleMatchesErr = contract.Error("old_string appears multiple times in the file. Please provide more context to ensure a unique match, or set replace_all to true")
)

//go:embed edit.md
var editDescription []byte

type editContext struct {
	ctx         context.Context
	permissions permission.Service
	files       history.Service
	filetracker filetracker.Service
	workingDir  string
}

// NewEditPortTool is the framework-agnostic form of the edit tool.
func NewEditPortTool(
	lspManager *lsp.Manager,
	permissions permission.Service,
	files history.Service,
	filetracker filetracker.Service,
	workingDir string,
) contract.Tool {
	return contract.NewFunc(
		EditToolName,
		toolutil.FirstLineDescription(editDescription),
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"file_path": map[string]any{
					"type":        "string",
					"description": "The absolute path to the file to modify",
				},
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
					"description": "Replace all occurrences of old_string (default false)",
				},
			},
			"required": []string{"file_path", "old_string", "new_string"},
		},
		func(ctx context.Context, _ contract.ToolContext, args json.RawMessage) (contract.ToolResult, error) {
			var params EditParams
			if err := json.Unmarshal(args, &params); err != nil {
				return contract.ToolResult{}, fmt.Errorf("invalid parameters: %w", err)
			}
			callID, _ := toolutil.ToolCallIDFromCtx(ctx)
			return runEdit(ctx, lspManager, permissions, files, filetracker, workingDir, params, callID)
		},
	)
}

// runEdit dispatches to create, delete, or replace and appends diagnostics.
func runEdit(
	ctx context.Context,
	lspManager *lsp.Manager,
	permissions permission.Service,
	files history.Service,
	filetracker filetracker.Service,
	workingDir string,
	params EditParams,
	callID string,
) (contract.ToolResult, error) {
	if params.FilePath == "" {
		return contract.Error("file_path is required"), nil
	}

	params.FilePath = ext.SmartJoin(workingDir, params.FilePath)

	var response contract.ToolResult
	var err error

	editCtx := editContext{ctx, permissions, files, filetracker, workingDir}

	if params.OldString == "" {
		response, err = createNewFile(editCtx, params.FilePath, params.NewString, callID)
	} else if params.NewString == "" {
		response, err = deleteContent(editCtx, params.FilePath, params.OldString, params.ReplaceAll, callID)
	} else {
		response, err = replaceContent(editCtx, params.FilePath, params.OldString, params.NewString, params.ReplaceAll, callID)
	}

	if err != nil {
		return response, err
	}
	// A tool-level failure (bad edit) returns early without LSP diagnostics
	// processing.
	if response.Error == nil {
		lsp.NotifyLSPs(ctx, lspManager, params.FilePath)

		text := fmt.Sprintf("<result>\n%s\n</result>\n", response.Content)
		text += lsp.GetDiagnostics(params.FilePath, lspManager)
		response.Content = text
	}
	return response, nil
}

func createNewFile(edit editContext, filePath, content string, callID string) (contract.ToolResult, error) {
	fileInfo, err := os.Stat(filePath)
	if err == nil {
		if fileInfo.IsDir() {
			return contract.Error(fmt.Sprintf("path is a directory, not a file: %s", filePath)), nil
		}
		return contract.Error(fmt.Sprintf("file already exists: %s", filePath)), nil
	} else if !os.IsNotExist(err) {
		return contract.ToolResult{}, fmt.Errorf("failed to access file: %w", err)
	}

	dir := filepath.Dir(filePath)
	if err = os.MkdirAll(dir, 0o750); err != nil {
		return contract.ToolResult{}, fmt.Errorf("failed to create parent directories: %w", err)
	}

	sessionID := toolutil.GetSessionFromContext(edit.ctx)
	if sessionID == "" {
		return contract.ToolResult{}, fmt.Errorf("session ID is required for creating a new file")
	}

	_, additions, removals := diff.GenerateDiff(
		"",
		content,
		strings.TrimPrefix(filePath, edit.workingDir),
	)
	p, err := edit.permissions.Request(edit.ctx,
		permission.CreatePermissionRequest{
			SessionID:   sessionID,
			Path:        fsext.PathOrPrefix(filePath, edit.workingDir),
			ToolCallID:  callID,
			ToolName:    EditToolName,
			Action:      "write",
			Description: fmt.Sprintf("Create file %s", filePath),
			Params: EditPermissionsParams{
				FilePath:   filePath,
				OldContent: "",
				NewContent: content,
			},
		},
	)
	if err != nil {
		return contract.ToolResult{}, err
	}
	if !p {
		return permissionDeniedResult(), nil
	}

	err = os.WriteFile(filePath, []byte(content), 0o600)
	if err != nil {
		return contract.ToolResult{}, fmt.Errorf("failed to write file: %w", err)
	}

	// File can't be in the history so we create a new file history
	_, err = edit.files.Create(edit.ctx, sessionID, filePath, "")
	if err != nil {
		// Log error but don't fail the operation
		return contract.ToolResult{}, fmt.Errorf("error creating file history: %w", err)
	}

	// Add the new content to the file history
	_, err = edit.files.CreateVersion(edit.ctx, sessionID, filePath, content)
	if err != nil {
		// Log error but don't fail the operation
		slog.Error("Error creating file history version", "error", err)
	}

	edit.filetracker.RecordRead(edit.ctx, sessionID, filePath)

	return contract.WithMetadata(contract.Text("File created: "+filePath), EditResponseMetadata{
		OldContent: "",
		NewContent: content,
		Additions:  additions,
		Removals:   removals,
	}), nil
}

func deleteContent(edit editContext, filePath, oldString string, replaceAll bool, callID string) (contract.ToolResult, error) {
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return contract.Error(fmt.Sprintf("file not found: %s", filePath)), nil
		}
		return contract.ToolResult{}, fmt.Errorf("failed to access file: %w", err)
	}

	if fileInfo.IsDir() {
		return contract.Error(fmt.Sprintf("path is a directory, not a file: %s", filePath)), nil
	}

	sessionID := toolutil.GetSessionFromContext(edit.ctx)
	if sessionID == "" {
		return contract.ToolResult{}, fmt.Errorf("session ID is required for deleting content")
	}

	lastRead := edit.filetracker.LastReadTime(edit.ctx, sessionID, filePath)
	if lastRead.IsZero() {
		return contract.Error("you must read the file before editing it. Use the View tool first"), nil
	}

	modTime := fileInfo.ModTime().Truncate(time.Second)
	if modTime.After(lastRead) {
		return contract.Error(
			fmt.Sprintf("file %s has been modified since it was last read (mod time: %s, last read: %s)",
				filePath, modTime.Format(time.RFC3339), lastRead.Format(time.RFC3339),
			)), nil
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return contract.ToolResult{}, fmt.Errorf("failed to read file: %w", err)
	}

	oldContent, isCrlf := fsext.ToUnixLineEndings(string(content))

	var newContent string

	if replaceAll {
		newContent = strings.ReplaceAll(oldContent, oldString, "")
		if newContent == oldContent {
			return oldStringNotFoundErr, nil
		}
	} else {
		index := strings.Index(oldContent, oldString)
		if index == -1 {
			return oldStringNotFoundErr, nil
		}

		lastIndex := strings.LastIndex(oldContent, oldString)
		if index != lastIndex {
			return contract.Error("old_string appears multiple times in the file. Please provide more context to ensure a unique match, or set replace_all to true"), nil
		}

		newContent = oldContent[:index] + oldContent[index+len(oldString):]
	}

	_, additions, removals := diff.GenerateDiff(
		oldContent,
		newContent,
		strings.TrimPrefix(filePath, edit.workingDir),
	)

	p, err := edit.permissions.Request(edit.ctx,
		permission.CreatePermissionRequest{
			SessionID:   sessionID,
			Path:        fsext.PathOrPrefix(filePath, edit.workingDir),
			ToolCallID:  callID,
			ToolName:    EditToolName,
			Action:      "write",
			Description: fmt.Sprintf("Delete content from file %s", filePath),
			Params: EditPermissionsParams{
				FilePath:   filePath,
				OldContent: oldContent,
				NewContent: newContent,
			},
		},
	)
	if err != nil {
		return contract.ToolResult{}, err
	}
	if !p {
		return permissionDeniedResult(), nil
	}

	if isCrlf {
		newContent, _ = fsext.ToWindowsLineEndings(newContent)
	}

	err = os.WriteFile(filePath, []byte(newContent), 0o600)
	if err != nil {
		return contract.ToolResult{}, fmt.Errorf("failed to write file: %w", err)
	}

	// Check if file exists in history
	file, err := edit.files.GetByPathAndSession(edit.ctx, filePath, sessionID)
	if err != nil {
		_, err = edit.files.Create(edit.ctx, sessionID, filePath, oldContent)
		if err != nil {
			// Log error but don't fail the operation
			return contract.ToolResult{}, fmt.Errorf("error creating file history: %w", err)
		}
	}
	if file.Content != oldContent {
		// User manually changed the content; store an intermediate version
		_, err = edit.files.CreateVersion(edit.ctx, sessionID, filePath, oldContent)
		if err != nil {
			slog.Error("Error creating file history version", "error", err)
		}
	}
	// Store the new version
	_, err = edit.files.CreateVersion(edit.ctx, sessionID, filePath, newContent)
	if err != nil {
		slog.Error("Error creating file history version", "error", err)
	}

	edit.filetracker.RecordRead(edit.ctx, sessionID, filePath)

	return contract.WithMetadata(contract.Text("Content deleted from file: "+filePath), EditResponseMetadata{
		OldContent: oldContent,
		NewContent: newContent,
		Additions:  additions,
		Removals:   removals,
	}), nil
}

func replaceContent(edit editContext, filePath, oldString, newString string, replaceAll bool, callID string) (contract.ToolResult, error) {
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return contract.Error(fmt.Sprintf("file not found: %s", filePath)), nil
		}
		return contract.ToolResult{}, fmt.Errorf("failed to access file: %w", err)
	}

	if fileInfo.IsDir() {
		return contract.Error(fmt.Sprintf("path is a directory, not a file: %s", filePath)), nil
	}

	sessionID := toolutil.GetSessionFromContext(edit.ctx)
	if sessionID == "" {
		return contract.ToolResult{}, fmt.Errorf("session ID is required for edit a file")
	}

	lastRead := edit.filetracker.LastReadTime(edit.ctx, sessionID, filePath)
	if lastRead.IsZero() {
		return contract.Error("you must read the file before editing it. Use the View tool first"), nil
	}

	modTime := fileInfo.ModTime().Truncate(time.Second)
	if modTime.After(lastRead) {
		return contract.Error(
			fmt.Sprintf("file %s has been modified since it was last read (mod time: %s, last read: %s)",
				filePath, modTime.Format(time.RFC3339), lastRead.Format(time.RFC3339),
			)), nil
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return contract.ToolResult{}, fmt.Errorf("failed to read file: %w", err)
	}

	oldContent, isCrlf := fsext.ToUnixLineEndings(string(content))

	var newContent string

	if replaceAll {
		newContent = strings.ReplaceAll(oldContent, oldString, newString)
	} else {
		index := strings.Index(oldContent, oldString)
		if index == -1 {
			return oldStringNotFoundErr, nil
		}

		lastIndex := strings.LastIndex(oldContent, oldString)
		if index != lastIndex {
			return oldStringMultipleMatchesErr, nil
		}

		newContent = oldContent[:index] + newString + oldContent[index+len(oldString):]
	}

	if oldContent == newContent {
		return contract.Error("new content is the same as old content. No changes made."), nil
	}
	_, additions, removals := diff.GenerateDiff(
		oldContent,
		newContent,
		strings.TrimPrefix(filePath, edit.workingDir),
	)

	p, err := edit.permissions.Request(edit.ctx,
		permission.CreatePermissionRequest{
			SessionID:   sessionID,
			Path:        fsext.PathOrPrefix(filePath, edit.workingDir),
			ToolCallID:  callID,
			ToolName:    EditToolName,
			Action:      "write",
			Description: fmt.Sprintf("Replace content in file %s", filePath),
			Params: EditPermissionsParams{
				FilePath:   filePath,
				OldContent: oldContent,
				NewContent: newContent,
			},
		},
	)
	if err != nil {
		return contract.ToolResult{}, err
	}
	if !p {
		return permissionDeniedResult(), nil
	}

	if isCrlf {
		newContent, _ = fsext.ToWindowsLineEndings(newContent)
	}

	err = os.WriteFile(filePath, []byte(newContent), 0o600)
	if err != nil {
		return contract.ToolResult{}, fmt.Errorf("failed to write file: %w", err)
	}

	// Check if file exists in history
	file, err := edit.files.GetByPathAndSession(edit.ctx, filePath, sessionID)
	if err != nil {
		_, err = edit.files.Create(edit.ctx, sessionID, filePath, oldContent)
		if err != nil {
			// Log error but don't fail the operation
			return contract.ToolResult{}, fmt.Errorf("error creating file history: %w", err)
		}
	}
	if file.Content != oldContent {
		// User manually changed the content; store an intermediate version
		_, err = edit.files.CreateVersion(edit.ctx, sessionID, filePath, oldContent)
		if err != nil {
			slog.Debug("Error creating file history version", "error", err)
		}
	}
	// Store the new version
	_, err = edit.files.CreateVersion(edit.ctx, sessionID, filePath, newContent)
	if err != nil {
		slog.Error("Error creating file history version", "error", err)
	}

	edit.filetracker.RecordRead(edit.ctx, sessionID, filePath)

	return contract.WithMetadata(contract.Text("Content replaced in file: "+filePath), EditResponseMetadata{
		OldContent: oldContent,
		NewContent: newContent,
		Additions:  additions,
		Removals:   removals,
	}), nil
}
