// Package net hosts the web-bound builtin tools: fetch, crawl,
// web_fetch/web_search, download, download_docs and sourcegraph. The
// crawler/ subpackage is the shared crawling foundation; see the parent
// tools package doc for the tree-wide layout contract.
package web

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/core/tools/contract"
	netcommon "github.com/nextsko/mocode-agent/internal/core/tools/external/common/net"
	"github.com/nextsko/mocode-agent/internal/util/errcoll"

	md "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/PuerkitoBio/goquery"

	"github.com/nextsko/mocode-agent/internal/core/permission"
)

const (
	FetchToolName = "fetch"
	MaxFetchSize  = 100 * 1024 // 100KB
)

type FetchParams = netcommon.FetchParams

type FetchPermissionsParams = netcommon.FetchPermissionsParams

//go:embed fetch.md
var fetchDescription []byte

// NewFetchPortTool is the framework-agnostic form of the fetch tool.
func NewFetchPortTool(permissions permission.Service, workingDir string, client *http.Client) contract.Tool {
	if client == nil {
		client = netcommon.DefaultHTTPClient()
	}

	return asParallel(contract.NewFunc(
		FetchToolName,
		toolutil.FirstLineDescription(fetchDescription),
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url": map[string]any{
					"type":        "string",
					"description": "The URL to fetch content from",
				},
				"format": map[string]any{
					"type":        "string",
					"description": "The format to return the content in (text, markdown, or html)",
				},
				"timeout": map[string]any{
					"type":        "integer",
					"description": "Optional timeout in seconds (max 120)",
				},
			},
			"required": []string{"url", "format"},
		},
		func(ctx context.Context, _ contract.ToolContext, args json.RawMessage) (contract.ToolResult, error) {
			var params FetchParams
			if err := json.Unmarshal(args, &params); err != nil {
				return contract.ToolResult{}, fmt.Errorf("invalid parameters: %w", err)
			}
			callID, _ := toolutil.ToolCallIDFromCtx(ctx)
			return runFetch(ctx, permissions, workingDir, client, params, callID)
		},
	))
}

// runFetch validates the request, asks for permission and renders the URL
// content in the requested format.
func runFetch(ctx context.Context, permissions permission.Service, workingDir string, client *http.Client, params FetchParams, callID string) (contract.ToolResult, error) {
	record := func(category errcoll.ErrorCategory, _ error, msg string) {
		if c := errcoll.FromContext(ctx); c != nil {
			c.Record(errcoll.ErrorRecord{
				SessionID: toolutil.GetSessionFromContext(ctx),
				ToolName:  FetchToolName,
				Error:     msg,
				Category:  category,
			})
		}
	}

	if params.URL == "" {
		return contract.Error("URL parameter is required"), nil
	}

	format := strings.ToLower(params.Format)
	if format != "text" && format != "markdown" && format != "html" {
		return contract.Error("Format must be one of: text, markdown, html"), nil
	}

	if !strings.HasPrefix(params.URL, "http://") && !strings.HasPrefix(params.URL, "https://") {
		return contract.Error("URL must start with http:// or https://"), nil
	}

	sessionID := toolutil.GetSessionFromContext(ctx)
	if sessionID == "" {
		msg := "session ID is required for fetch"
		record(errcoll.CategoryToolExecution, nil, msg)
		return contract.Error(msg), nil
	}

	p, err := permissions.Request(
		ctx,
		permission.CreatePermissionRequest{
			SessionID:   sessionID,
			Path:        workingDir,
			ToolCallID:  callID,
			ToolName:    FetchToolName,
			Action:      "fetch",
			Description: fmt.Sprintf("Fetch content from URL: %s", params.URL),
			Params:      FetchPermissionsParams(params),
		},
	)
	if err != nil {
		msg := "Permission request failed: " + err.Error()
		record(errcoll.CategoryPermission, err, msg)
		return contract.Error(msg), nil
	}
	if !p {
		record(errcoll.CategoryPermission, nil, "permission denied")
		return permissionDeniedResult(), nil
	}

	// maxFetchTimeoutSeconds is the maximum allowed timeout for fetch requests (2 minutes)
	const maxFetchTimeoutSeconds = 120

	// Handle timeout with context
	requestCtx := ctx
	if params.Timeout > 0 {
		if params.Timeout > maxFetchTimeoutSeconds {
			params.Timeout = maxFetchTimeoutSeconds
		}
		var cancel context.CancelFunc
		requestCtx, cancel = context.WithTimeout(ctx, time.Duration(params.Timeout)*time.Second)
		defer cancel()
	}

	req, err := http.NewRequestWithContext(requestCtx, "GET", params.URL, nil)
	if err != nil {
		msg := fmt.Sprintf("failed to create request: %v", err)
		record(errcoll.CategoryToolExecution, err, msg)
		return contract.Error(msg), nil
	}

	req.Header.Set("User-Agent", netcommon.BrowserUserAgent)

	resp, err := client.Do(req)
	if err != nil {
		msg := fmt.Sprintf("failed to fetch URL: %v", err)
		record(errcoll.CategoryToolExecution, err, msg)
		return contract.Error(msg), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("Request failed with status code: %d", resp.StatusCode)
		record(errcoll.CategoryToolExecution, nil, msg)
		return contract.Error(msg), nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxFetchSize))
	if err != nil {
		msg := "Failed to read response body: " + err.Error()
		record(errcoll.CategoryToolExecution, err, msg)
		return contract.Error(msg), nil
	}

	content := string(body)

	validUTF8 := utf8.ValidString(content)
	if !validUTF8 {
		msg := "Response content is not valid UTF-8"
		record(errcoll.CategoryToolExecution, nil, msg)
		return contract.Error(msg), nil
	}
	contentType := resp.Header.Get("Content-Type")

	switch format {
	case "text":
		if strings.Contains(contentType, "text/html") {
			text, err := extractTextFromHTML(content)
			if err != nil {
				msg := "Failed to extract text from HTML: " + err.Error()
				record(errcoll.CategoryToolExecution, err, msg)
				return contract.Error(msg), nil
			}
			content = text
		}

	case "markdown":
		if strings.Contains(contentType, "text/html") {
			markdown, err := convertHTMLToMarkdown(content)
			if err != nil {
				msg := "Failed to convert HTML to Markdown: " + err.Error()
				record(errcoll.CategoryToolExecution, err, msg)
				return contract.Error(msg), nil
			}
			content = markdown
		}

		content = "```\n" + content + "\n```"

	case "html":
		// return only the body of the HTML document
		if strings.Contains(contentType, "text/html") {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(content))
			if err != nil {
				msg := "Failed to parse HTML: " + err.Error()
				record(errcoll.CategoryToolExecution, err, msg)
				return contract.Error(msg), nil
			}
			body, err := doc.Find("body").Html()
			if err != nil {
				msg := "Failed to extract body from HTML: " + err.Error()
				record(errcoll.CategoryToolExecution, err, msg)
				return contract.Error(msg), nil
			}
			if body == "" {
				msg := "No body content found in HTML"
				record(errcoll.CategoryToolExecution, nil, msg)
				return contract.Error(msg), nil
			}
			content = "<html>\n<body>\n" + body + "\n</body>\n</html>"
		}
	}
	// truncate content if it exceeds max read size
	if int64(len(content)) >= MaxFetchSize {
		content = content[:MaxFetchSize]
		content += fmt.Sprintf("\n\n[Content truncated to %d bytes]", MaxFetchSize)
	}

	return contract.Text(content), nil
}

func extractTextFromHTML(html string) (string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return "", fmt.Errorf("parse HTML: %w", err)
	}

	text := doc.Find("body").Text()
	text = strings.Join(strings.Fields(text), " ")

	return text, nil
}

func convertHTMLToMarkdown(html string) (string, error) {
	converter := md.NewConverter("", true, nil)

	markdown, err := converter.ConvertString(html)
	if err != nil {
		return "", fmt.Errorf("convert HTML to markdown: %w", err)
	}

	return markdown, nil
}
