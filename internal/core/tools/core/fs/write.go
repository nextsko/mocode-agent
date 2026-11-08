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

	"github.com/nextsko/mocode-agent/internal/core/acpfs"
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

//go:embed write.md
var writeDescription []byte

type WriteParams struct {
	FilePath string `json:"file_path" description:"The path to the file to write"`
	Content  string `json:"content" description:"The content to write to the file"`
}

type WritePermissionsParams struct {
	FilePath   string `json:"file_path"`
	OldContent string `json:"old_content,omitempty"`
	NewContent string `json:"new_content,omitempty"`
}

type WriteResponseMetadata struct {
	Diff      string `json:"diff"`
	Additions int    `json:"additions"`
	Removals  int    `json:"removals"`
}

const WriteToolName = "write"

// NewWritePortTool is the framework-agnostic form of the write tool.
func NewWritePortTool(
	lspManager *lsp.Manager,
	permissions permission.Service,
	files history.Service,
	filetracker filetracker.Service,
	workingDir string,
) contract.Tool {
	return contract.NewFunc(
		WriteToolName,
		toolutil.FirstLineDescription(writeDescription),
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"file_path": map[string]any{
					"type":        "string",
					"description": "The path to the file to write",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "The content to write to the file",
				},
			},
			"required": []string{"file_path", "content"},
		},
		func(ctx context.Context, _ contract.ToolContext, args json.RawMessage) (contract.ToolResult, error) {
			var params WriteParams
			if err := json.Unmarshal(args, &params); err != nil {
				return contract.ToolResult{}, fmt.Errorf("invalid parameters: %w", err)
			}
			callID, _ := toolutil.ToolCallIDFromCtx(ctx)
			return runWrite(ctx, lspManager, permissions, files, filetracker, workingDir, params, callID)
		},
	)
}

// runWrite validates the target, asks for permission, writes the file, and
// records history plus LSP diagnostics.
func runWrite(
	ctx context.Context,
	lspManager *lsp.Manager,
	permissions permission.Service,
	files history.Service,
	filetracker filetracker.Service,
	workingDir string,
	params WriteParams,
	callID string,
) (contract.ToolResult, error) {
	if params.FilePath == "" {
		return contract.Error("file_path is required"), nil
	}

	if params.Content == "" {
		return contract.Error("content is required"), nil
	}

	sessionID := toolutil.GetSessionFromContext(ctx)
	if sessionID == "" {
		return contract.ToolResult{}, fmt.Errorf("session_id is required")
	}

	filePath := ext.SmartJoin(workingDir, params.FilePath)

	// ACP client filesystem: virtual files are read and written through the
	// client, so local stat/history checks on disk do not apply.
	if acpfs.Has(sessionID) {
		return writeViaClientFS(ctx, permissions, files, filetracker, workingDir, sessionID, filePath, params, callID)
	}

	fileInfo, err := os.Stat(filePath)
	if err == nil {
		if fileInfo.IsDir() {
			return contract.Error(fmt.Sprintf("Path is a directory, not a file: %s", filePath)), nil
		}

		modTime := fileInfo.ModTime().Truncate(time.Second)
		lastRead := filetracker.LastReadTime(ctx, sessionID, filePath)
		if modTime.After(lastRead) {
			return contract.Error(fmt.Sprintf("File %s has been modified since it was last read.\nLast modification: %s\nLast read: %s\n\nPlease read the file again before modifying it.",
				filePath, modTime.Format(time.RFC3339), lastRead.Format(time.RFC3339))), nil
		}

		oldContent, readErr := os.ReadFile(filePath)
		if readErr == nil && string(oldContent) == params.Content {
			return contract.Error(fmt.Sprintf("File %s already contains the exact content. No changes made.", filePath)), nil
		}
	} else if !os.IsNotExist(err) {
		return contract.ToolResult{}, fmt.Errorf("error checking file: %w", err)
	}

	dir := filepath.Dir(filePath)
	if err = os.MkdirAll(dir, 0o750); err != nil {
		return contract.ToolResult{}, fmt.Errorf("error creating directory: %w", err)
	}

	oldContent := ""
	if fileInfo != nil && !fileInfo.IsDir() {
		oldBytes, readErr := os.ReadFile(filePath)
		if readErr == nil {
			oldContent = string(oldBytes)
		}
	}

	diff, additions, removals := diff.GenerateDiff(
		oldContent,
		params.Content,
		strings.TrimPrefix(filePath, workingDir),
	)

	p, err := permissions.Request(ctx,
		permission.CreatePermissionRequest{
			SessionID:   sessionID,
			Path:        fsext.PathOrPrefix(filePath, workingDir),
			ToolCallID:  callID,
			ToolName:    WriteToolName,
			Action:      WriteToolName,
			Description: fmt.Sprintf("Create file %s", filePath),
			Params: WritePermissionsParams{
				FilePath:   filePath,
				OldContent: oldContent,
				NewContent: params.Content,
			},
		},
	)
	if err != nil {
		return contract.ToolResult{}, err
	}
	if !p {
		return permissionDeniedResult(), nil
	}

	err = os.WriteFile(filePath, []byte(params.Content), 0o600)
	if err != nil {
		return contract.ToolResult{}, fmt.Errorf("error writing file: %w", err)
	}

	// Check if file exists in history
	file, err := files.GetByPathAndSession(ctx, filePath, sessionID)
	if err != nil {
		_, err = files.Create(ctx, sessionID, filePath, oldContent)
		if err != nil {
			// Log error but don't fail the operation
			return contract.ToolResult{}, fmt.Errorf("error creating file history: %w", err)
		}
	}
	if file.Content != oldContent {
		// User manually changed the content; store an intermediate version
		_, err = files.CreateVersion(ctx, sessionID, filePath, oldContent)
		if err != nil {
			slog.Error("Error creating file history version", "error", err)
		}
	}
	// Store the new version
	_, err = files.CreateVersion(ctx, sessionID, filePath, params.Content)
	if err != nil {
		slog.Error("Error creating file history version", "error", err)
	}

	filetracker.RecordRead(ctx, sessionID, filePath)

	lsp.NotifyLSPs(ctx, lspManager, params.FilePath)

	result := fmt.Sprintf("File successfully written: %s", filePath)
	result = fmt.Sprintf("<result>\n%s\n</result>", result)
	result += lsp.GetDiagnostics(filePath, lspManager)
	return contract.WithMetadata(contract.Text(result), WriteResponseMetadata{
		Diff:      diff,
		Additions: additions,
		Removals:  removals,
	}), nil
}

// writeViaClientFS writes a virtual file through the ACP client filesystem,
// keeping permission gating and history but skipping local disk semantics.
func writeViaClientFS(
	ctx context.Context,
	permissions permission.Service,
	files history.Service,
	filetracker filetracker.Service,
	workingDir, sessionID, filePath string,
	params WriteParams,
	callID string,
) (contract.ToolResult, error) {
	oldContent, _, readErr := acpfs.Read(ctx, sessionID, filePath)
	if readErr != nil {
		oldContent = ""
	}
	diff, additions, removals := diff.GenerateDiff(
		oldContent,
		params.Content,
		strings.TrimPrefix(filePath, workingDir),
	)
	granted, permErr := permissions.Request(ctx, permission.CreatePermissionRequest{
		SessionID:   sessionID,
		Path:        fsext.PathOrPrefix(filePath, workingDir),
		ToolCallID:  callID,
		ToolName:    WriteToolName,
		Action:      WriteToolName,
		Description: fmt.Sprintf("Write file through ACP client: %s", filePath),
		Params: WritePermissionsParams{
			FilePath:   filePath,
			OldContent: oldContent,
			NewContent: params.Content,
		},
	})
	if permErr != nil {
		return contract.ToolResult{}, permErr
	}
	if !granted {
		return permissionDeniedResult(), nil
	}
	ok, writeErr := acpfs.Write(ctx, sessionID, filePath, params.Content)
	if !ok {
		return contract.Error("ACP client filesystem is no longer available; retry the write"), nil
	}
	if writeErr != nil {
		return contract.Error(writeErr.Error()), nil
	}
	file, histErr := files.GetByPathAndSession(ctx, filePath, sessionID)
	if histErr != nil {
		_, _ = files.Create(ctx, sessionID, filePath, oldContent)
	} else if file.Content != oldContent {
		_, _ = files.CreateVersion(ctx, sessionID, filePath, oldContent)
	}
	_, _ = files.CreateVersion(ctx, sessionID, filePath, params.Content)
	filetracker.RecordRead(ctx, sessionID, filePath)

	result := fmt.Sprintf("<result>\nFile successfully written via the ACP client filesystem: %s\n(local disk is not updated; do not verify with bash)\n</result>", filePath)
	return contract.WithMetadata(contract.Text(result), WriteResponseMetadata{
		Diff: diff, Additions: additions, Removals: removals,
	}), nil
}
