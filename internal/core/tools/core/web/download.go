package web

import (
	"cmp"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/core/tools/contract"
	netcommon "github.com/nextsko/mocode-agent/internal/core/tools/external/common/net"

	"github.com/nextsko/mocode-agent/internal/core/permission"
	"github.com/nextsko/mocode-agent/internal/util/ext"
)

type DownloadParams struct {
	URL      string `json:"url" description:"The URL to download from"`
	FilePath string `json:"file_path" description:"The local file path where the downloaded content should be saved"`
	Timeout  int    `json:"timeout,omitempty" description:"Optional timeout in seconds (max 600)"`
}

type DownloadPermissionsParams struct {
	URL      string `json:"url"`
	FilePath string `json:"file_path"`
	Timeout  int    `json:"timeout,omitempty"`
}

const DownloadToolName = "download"

//go:embed download.md
var downloadDescription []byte

// NewDownloadPortTool is the framework-agnostic form of the download tool.
func NewDownloadPortTool(permissions permission.Service, workingDir string, client *http.Client) contract.Tool {
	if client == nil {
		client = netcommon.NewHTTPClient(5 * time.Minute) // 5 minute timeout for downloads
	}
	return asParallel(contract.NewFunc(
		DownloadToolName,
		toolutil.FirstLineDescription(downloadDescription),
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url": map[string]any{
					"type":        "string",
					"description": "The URL to download from",
				},
				"file_path": map[string]any{
					"type":        "string",
					"description": "The local file path where the downloaded content should be saved",
				},
				"timeout": map[string]any{
					"type":        "integer",
					"description": "Optional timeout in seconds (max 600)",
				},
			},
			"required": []string{"url", "file_path"},
		},
		func(ctx context.Context, _ contract.ToolContext, args json.RawMessage) (contract.ToolResult, error) {
			var params DownloadParams
			if err := json.Unmarshal(args, &params); err != nil {
				return contract.ToolResult{}, fmt.Errorf("invalid parameters: %w", err)
			}
			return runDownload(ctx, permissions, workingDir, client, params)
		},
	))
}

// runDownload streams the URL into the requested workspace-relative path.
func runDownload(ctx context.Context, permissions permission.Service, workingDir string, client *http.Client, params DownloadParams) (contract.ToolResult, error) {
	if params.URL == "" {
		return contract.Error("URL parameter is required"), nil
	}

	if params.FilePath == "" {
		return contract.Error("file_path parameter is required"), nil
	}

	if !strings.HasPrefix(params.URL, "http://") && !strings.HasPrefix(params.URL, "https://") {
		return contract.Error("URL must start with http:// or https://"), nil
	}

	filePath := ext.SmartJoin(workingDir, params.FilePath)
	relPath, _ := filepath.Rel(workingDir, filePath)
	relPath = filepath.ToSlash(cmp.Or(relPath, filePath))

	sessionID := toolutil.GetSessionFromContext(ctx)
	if sessionID == "" {
		return contract.ToolResult{}, fmt.Errorf("session ID is required for downloading files")
	}

	p, err := permissions.Request(
		ctx,
		permission.CreatePermissionRequest{
			SessionID:   sessionID,
			Path:        filePath,
			ToolName:    DownloadToolName,
			Action:      "download",
			Description: fmt.Sprintf("Download file from URL: %s to %s", params.URL, filePath),
			Params:      DownloadPermissionsParams(params),
		},
	)
	if err != nil {
		return contract.ToolResult{}, err
	}
	if !p {
		return permissionDeniedResult(), nil
	}

	// Handle timeout with context
	requestCtx := ctx
	if params.Timeout > 0 {
		maxTimeout := 600 // 10 minutes
		if params.Timeout > maxTimeout {
			params.Timeout = maxTimeout
		}
		var cancel context.CancelFunc
		requestCtx, cancel = context.WithTimeout(ctx, time.Duration(params.Timeout)*time.Second)
		defer cancel()
	}

	req, err := http.NewRequestWithContext(requestCtx, "GET", params.URL, nil)
	if err != nil {
		return contract.ToolResult{}, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", netcommon.BrowserUserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return contract.ToolResult{}, fmt.Errorf("failed to download from URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return contract.Error(fmt.Sprintf("Request failed with status code: %d", resp.StatusCode)), nil
	}

	// Create parent directories if they don't exist
	if err := os.MkdirAll(filepath.Dir(filePath), 0o750); err != nil {
		return contract.ToolResult{}, fmt.Errorf("failed to create parent directories: %w", err)
	}

	// Create the output file
	outFile, err := os.Create(filePath)
	if err != nil {
		return contract.ToolResult{}, fmt.Errorf("failed to create output file: %w", err)
	}
	defer outFile.Close()

	// Copy data without an explicit size limit.
	// The overall download is still constrained by the HTTP client's timeout
	// and any upstream server limits.
	bytesWritten, err := io.Copy(outFile, resp.Body)
	if err != nil {
		return contract.ToolResult{}, fmt.Errorf("failed to write file: %w", err)
	}

	contentType := resp.Header.Get("Content-Type")
	responseMsg := fmt.Sprintf("Successfully downloaded %d bytes to %s", bytesWritten, relPath)
	if contentType != "" {
		responseMsg += fmt.Sprintf(" (Content-Type: %s)", contentType)
	}

	return contract.Text(responseMsg), nil
}
